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
/// - **End-of-utterance** transcripts go to stdout, prefixed by the session's
///   tag, on their own line. The session resets after each EOU so the next
///   utterance starts with empty state instead of appending forever.
actor ASRSession {
    /// Maximum characters of partial transcript shown in the live preview.
    /// Anything longer is left-truncated with a leading "…". Kept under typical
    /// terminal widths so the line never wraps; wrapping breaks `\r` overwrite.
    private static let previewTailChars = 100

    private let tag: String
    private let manager: StreamingEouAsrManager

    init(
        tag: String,
        chunkSize: StreamingChunkSize = .ms160,
        eouDebounceMs: Int = 600
    ) {
        self.tag = tag
        self.manager = StreamingEouAsrManager(
            configuration: MLModelConfiguration(),
            chunkSize: chunkSize,
            eouDebounceMs: eouDebounceMs
        )
    }

    func loadModels() async throws {
        try await manager.loadModels()

        let tag = self.tag
        await manager.setPartialCallback { partial in
            let trimmed = partial.trimmingCharacters(in: .whitespacesAndNewlines)
            let display = ASRSession.tailTruncate(trimmed, max: ASRSession.previewTailChars)
            // \r — return to column 0; \u{1B}[K — clear to end of (current) row.
            let line = "\r[\(tag) …] \(display)\u{1B}[K"
            FileHandle.standardError.write(Data(line.utf8))
        }

        await manager.setEouCallback { [weak self] final in
            FileHandle.standardError.write(Data("\r\u{1B}[K".utf8))
            let trimmed = final.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty {
                print("[\(tag)] \(trimmed)")
            }
            // EOU leaves accumulated state in place; reset for the next
            // utterance. The actor serializes this against in-flight appends.
            Task { await self?.resetAfterEou() }
        }
    }

    func append(_ buffer: AVAudioPCMBuffer) async throws {
        try await manager.appendAudio(buffer)
        try await manager.processBufferedAudio()
    }

    func finish() async throws {
        let final = try await manager.finish()
        FileHandle.standardError.write(Data("\r\u{1B}[K".utf8))
        let trimmed = final.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            print("[\(tag)] \(trimmed)")
        }
        await manager.reset()
    }

    private func resetAfterEou() async {
        await manager.reset()
    }

    private static func tailTruncate(_ s: String, max: Int) -> String {
        guard s.count > max else { return s }
        let suffix = s.suffix(max - 1)
        return "…" + suffix
    }
}
