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
    private var configChangeObserver: NSObjectProtocol?

    func start() throws -> AsyncThrowingStream<AVAudioPCMBuffer, Error> {
        precondition(!isRunning, "MicCapture.start() called while already running")

        let format = engine.inputNode.outputFormat(forBus: 0)
        guard format.sampleRate > 0 else {
            throw MicCaptureError.noInputDevice
        }

        let (stream, continuation) = AsyncThrowingStream<AVAudioPCMBuffer, Error>.makeStream(
            bufferingPolicy: .bufferingNewest(64)
        )
        self.continuation = continuation

        try installTapAndStart(format: format)

        // AVAudioEngine binds to whatever device is the default input at
        // start() time. If the user plugs in headphones, unplugs the
        // built-in mic, or switches default input in System Settings, the
        // engine keeps pulling from the original device and the stream
        // goes silent. macOS posts AVAudioEngineConfigurationChange on
        // every such transition — we tear down the tap and rebind to the
        // new default input. Downstream (VadGate's AVAudioConverter,
        // FluidAudio's internal AudioConverter) resamples per buffer, so
        // the new device's native format can differ freely.
        configChangeObserver = NotificationCenter.default.addObserver(
            forName: .AVAudioEngineConfigurationChange,
            object: engine,
            queue: .main
        ) { [weak self] _ in
            self?.handleConfigChange()
        }

        isRunning = true
        return stream
    }

    func stop() {
        guard isRunning else { return }
        if let observer = configChangeObserver {
            NotificationCenter.default.removeObserver(observer)
            configChangeObserver = nil
        }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        continuation?.finish()
        continuation = nil
        isRunning = false
    }

    private func installTapAndStart(format: AVAudioFormat) throws {
        engine.inputNode.installTap(
            onBus: 0,
            bufferSize: 1024,
            format: format
        ) { [weak self] buffer, _ in
            guard let cont = self?.continuation else { return }
            // Copy out of the engine-owned buffer; the tap may recycle it
            // as soon as this closure returns.
            let owned = MicCapture.copyOwned(of: buffer)
            cont.yield(owned)
        }
        try engine.start()
    }

    /// Re-bind the engine to whatever the new default input device is.
    /// Yielded buffers afterward carry the new device's native format;
    /// downstream consumers handle per-buffer format changes already.
    /// The only real failure is "no input device at all" (e.g. only
    /// device was disconnected with no fallback) — surface that as a
    /// stream error rather than spinning silently.
    private func handleConfigChange() {
        guard isRunning, let continuation = continuation else { return }

        engine.inputNode.removeTap(onBus: 0)
        engine.stop()

        let newFormat = engine.inputNode.outputFormat(forBus: 0)
        guard newFormat.sampleRate > 0 else {
            continuation.finish(throwing: MicCaptureError.noInputDevice)
            return
        }

        do {
            try installTapAndStart(format: newFormat)
        } catch {
            continuation.finish(throwing: error)
        }
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
