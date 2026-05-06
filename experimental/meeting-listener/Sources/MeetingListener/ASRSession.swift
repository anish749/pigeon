import AVFoundation
@preconcurrency import CoreML
import FluidAudio
import Foundation

/// Drives a single FluidAudio streaming ASR pipeline and emits transcripts to
/// the console.
///
/// - **Partial** transcripts go to stderr on a single self-overwriting line —
///   a live preview of what the model is currently hearing.
/// - **End-of-utterance** transcripts go to stdout, prefixed by the session's
///   tag, on their own line. The session resets after each EOU so the next
///   utterance starts with empty state instead of appending forever.
actor ASRSession {
    private let tag: String
    private let manager: StreamingEouAsrManager

    init(tag: String, chunkSize: StreamingChunkSize = .ms160) {
        self.tag = tag
        self.manager = StreamingEouAsrManager(
            configuration: MLModelConfiguration(),
            chunkSize: chunkSize
        )
    }

    func loadModels() async throws {
        try await manager.loadModels()

        let tag = self.tag
        await manager.setPartialCallback { partial in
            let trimmed = partial.trimmingCharacters(in: .whitespacesAndNewlines)
            // \r returns to start of line, \u{1B}[K clears to end of line so a
            // shrinking partial doesn't leave stale tail characters on screen.
            let line = "\r[\(tag) …] \(trimmed)\u{1B}[K"
            FileHandle.standardError.write(Data(line.utf8))
        }

        await manager.setEouCallback { [weak self] final in
            // Clear the live-preview line, then write the committed transcript.
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
}
