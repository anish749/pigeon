import AVFoundation
import Foundation

/// Captures audio from the default input device and forwards each
/// `AVAudioPCMBuffer` to the supplied callback.
///
/// FluidAudio's streaming manager accepts arbitrary input formats and handles
/// resampling internally, so we hand it the buffer the engine produces without
/// converting first.
final class MicCapture {
    private let engine = AVAudioEngine()
    private let onBuffer: (AVAudioPCMBuffer) -> Void
    private var isRunning = false

    init(onBuffer: @escaping (AVAudioPCMBuffer) -> Void) {
        self.onBuffer = onBuffer
    }

    func start() throws {
        let inputNode = engine.inputNode
        let format = inputNode.outputFormat(forBus: 0)
        guard format.sampleRate > 0 else {
            throw MicCaptureError.noInputDevice
        }

        inputNode.installTap(
            onBus: 0,
            bufferSize: 1024,
            format: format
        ) { [weak self] buffer, _ in
            self?.onBuffer(buffer)
        }

        try engine.start()
        isRunning = true
    }

    func stop() {
        guard isRunning else { return }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
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
