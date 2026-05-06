import AVFoundation
import FluidAudio
import Foundation

/// Drives a single FluidAudio streaming ASR pipeline and prints partial
/// transcripts to stdout, prefixed by the session's tag.
///
/// One source = one session. Concurrent calls to `append(_:)` are safe: they
/// fan out to the underlying actor, which processes buffered audio in order.
actor ASRSession {
    private let tag: String
    private let manager: any StreamingAsrManager
    private var lastPrinted = ""

    init(tag: String, variant: StreamingModelVariant = .parakeetEou160ms) {
        self.tag = tag
        self.manager = variant.createManager()
    }

    func loadModels() async throws {
        try await manager.loadModels()
        await manager.setPartialTranscriptCallback { [tag] partial in
            // Print whenever the partial transcript grows. The callback runs on
            // FluidAudio's actor; printing from here is fine.
            let trimmed = partial.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { return }
            print("[\(tag)] \(trimmed)")
        }
    }

    func append(_ buffer: AVAudioPCMBuffer) async throws {
        try await manager.appendAudio(buffer)
        try await manager.processBufferedAudio()
    }

    func finish() async throws {
        let final = try await manager.finish()
        let trimmed = final.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty && trimmed != lastPrinted {
            print("[\(tag)] \(trimmed)")
        }
        lastPrinted = trimmed
        try await manager.reset()
    }
}
