import AVFoundation
@preconcurrency import CoreML
import FluidAudio
import Foundation

/// Drives a single FluidAudio streaming ASR pipeline and emits transcripts to
/// the console.
///
/// - **Partial** transcripts go to stderr on a single self-overwriting line —
///   a live preview of what the model is currently hearing. Truncated from the
///   left so it always fits on one row regardless of utterance length.
/// - **Committed** transcripts go to stdout, prefixed by the session's tag,
///   on their own line, when our `VadGate` (FluidAudio's Silero VAD) reports a
///   speech-end event. We deliberately do not use FluidAudio's
///   `setEouCallback` — on real microphones with low-level background noise
///   the model keeps emitting tokens through "silence" and the EOU debounce
///   never confirms. A trained VAD trained to distinguish speech from
///   non-speech is the right tool.
actor ASRSession {
    /// Maximum characters of partial transcript shown in the live preview.
    /// Anything longer is left-truncated with a leading "…". Kept under typical
    /// terminal widths so the line never wraps; wrapping breaks `\r` overwrite.
    private static let previewTailChars = 100

    private let tag: String
    private let manager: StreamingEouAsrManager
    private let vad: VadGate

    init(tag: String, chunkSize: StreamingChunkSize = .ms160) {
        self.tag = tag
        self.manager = StreamingEouAsrManager(
            configuration: MLModelConfiguration(),
            chunkSize: chunkSize
        )
        self.vad = VadGate()
    }

    func loadModels() async throws {
        try await manager.loadModels()
        try await vad.loadModel()

        let tag = self.tag
        await manager.setPartialCallback { partial in
            let trimmed = partial.trimmingCharacters(in: .whitespacesAndNewlines)
            let display = ASRSession.tailTruncate(trimmed, max: ASRSession.previewTailChars)
            // \r — return to column 0; \u{1B}[K — clear to end of (current) row.
            let line = "\r[\(tag) …] \(display)\u{1B}[K"
            FileHandle.standardError.write(Data(line.utf8))
        }
    }

    func append(_ buffer: AVAudioPCMBuffer) async throws {
        let speechEnded = try await vad.observe(buffer)
        try await manager.appendAudio(buffer)
        try await manager.processBufferedAudio()
        if speechEnded {
            try await commit()
        }
    }

    /// Flush whatever is buffered and reset. Call at end-of-stream (e.g. after
    /// reading a file or on Ctrl-C) so a trailing utterance isn't lost.
    func finish() async throws {
        try await commit()
    }

    private func commit() async throws {
        let final = try await manager.finish()
        FileHandle.standardError.write(Data("\r\u{1B}[K".utf8))
        let trimmed = final.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            print("[\(tag)] \(trimmed)")
        }
        await manager.reset()
    }

    private static func tailTruncate(_ s: String, max: Int) -> String {
        guard s.count > max else { return s }
        let suffix = s.suffix(max - 1)
        return "…" + suffix
    }
}
