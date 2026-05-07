import AVFoundation
import Foundation

/// Captures audio from the default input device and surfaces buffers as an
/// `AsyncStream` so callers can `try await` ASR work and have errors
/// propagate up the call chain rather than being swallowed inside a tap
/// callback.
///
/// Buffers are **copied** out of the engine-owned memory before being yielded
/// so consumers may hold them across actor hops; AVAudioEngine's tap-supplied
/// buffer lifetime is undocumented and we don't want to rely on it.
final class MicCapture: AudioSource, @unchecked Sendable {
    private let engine = AVAudioEngine()
    private var continuation: AsyncThrowingStream<AVAudioPCMBuffer, Error>.Continuation?
    private var isRunning = false

    func start() throws -> AsyncThrowingStream<AVAudioPCMBuffer, Error> {
        precondition(!isRunning, "MicCapture.start() called while already running")

        let inputNode = engine.inputNode
        let format = inputNode.outputFormat(forBus: 0)
        guard format.sampleRate > 0 else {
            throw MicCaptureError.noInputDevice
        }

        let (stream, continuation) = AsyncThrowingStream<AVAudioPCMBuffer, Error>.makeStream(
            bufferingPolicy: .bufferingNewest(64)
        )
        self.continuation = continuation

        inputNode.installTap(
            onBus: 0,
            bufferSize: 1024,
            format: format
        ) { buffer, _ in
            // Copy out of the engine-owned buffer; the tap may recycle it
            // as soon as this closure returns.
            let owned = MicCapture.copyOwned(of: buffer)
            continuation.yield(owned)
        }

        try engine.start()
        isRunning = true
        return stream
    }

    func stop() {
        guard isRunning else { return }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        continuation?.finish()
        continuation = nil
        isRunning = false
    }

    /// Allocates a new `AVAudioPCMBuffer` in the same format as `src` and
    /// copies the audio data over. Crashes loudly if allocation fails — the
    /// only realistic cause is system-wide OOM, where silent skipping would
    /// just hide the underlying problem.
    private static func copyOwned(of src: AVAudioPCMBuffer) -> AVAudioPCMBuffer {
        guard let dst = AVAudioPCMBuffer(
            pcmFormat: src.format,
            frameCapacity: src.frameLength
        ) else {
            preconditionFailure("AVAudioPCMBuffer allocation failed in mic tap callback")
        }
        dst.frameLength = src.frameLength

        let frameCount = Int(src.frameLength)
        let channelCount = Int(src.format.channelCount)

        if let srcChannels = src.floatChannelData,
           let dstChannels = dst.floatChannelData {
            for ch in 0..<channelCount {
                memcpy(
                    dstChannels[ch],
                    srcChannels[ch],
                    frameCount * MemoryLayout<Float>.size
                )
            }
        } else if let srcInt16 = src.int16ChannelData,
                  let dstInt16 = dst.int16ChannelData {
            for ch in 0..<channelCount {
                memcpy(
                    dstInt16[ch],
                    srcInt16[ch],
                    frameCount * MemoryLayout<Int16>.size
                )
            }
        } else {
            preconditionFailure(
                "Unsupported PCM format in mic tap: \(src.format)"
            )
        }
        return dst
    }
}

enum MicCaptureError: Error, CustomStringConvertible {
    case noInputDevice

    var description: String {
        switch self {
        case .noInputDevice:
            return "No input audio device available (sample rate is 0)."
        }
    }
}
