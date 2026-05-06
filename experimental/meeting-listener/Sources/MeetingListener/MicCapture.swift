import AVFoundation
import Foundation

/// Captures audio from the default input device and surfaces buffers as an
/// `AsyncStream`. The stream lets callers `try await` ASR work and have any
/// errors propagate up the call chain rather than being swallowed inside an
/// audio-tap callback.
///
/// FluidAudio's streaming manager accepts arbitrary input formats and handles
/// resampling internally, so we forward buffers in the engine's native format.
final class MicCapture {
    private let engine = AVAudioEngine()
    private var continuation: AsyncStream<AVAudioPCMBuffer>.Continuation?
    private var isRunning = false

    /// Starts the engine and returns a stream of buffers. The stream finishes
    /// when `stop()` is called. Buffers are dropped on overflow rather than
    /// queued unboundedly — ASR running slower than realtime should degrade
    /// gracefully, not OOM.
    func start() throws -> AsyncStream<AVAudioPCMBuffer> {
        precondition(!isRunning, "MicCapture.start() called while already running")

        let inputNode = engine.inputNode
        let format = inputNode.outputFormat(forBus: 0)
        guard format.sampleRate > 0 else {
            throw MicCaptureError.noInputDevice
        }

        let (stream, continuation) = AsyncStream<AVAudioPCMBuffer>.makeStream(
            bufferingPolicy: .bufferingNewest(64)
        )
        self.continuation = continuation

        inputNode.installTap(
            onBus: 0,
            bufferSize: 1024,
            format: format
        ) { buffer, _ in
            continuation.yield(buffer)
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
