import AVFoundation
@preconcurrency import CoreML
import Darwin
import FluidAudio
import Foundation

/// Drives a single FluidAudio streaming ASR pipeline and emits transcripts to
/// the console.
///
/// - **Partial** transcripts go to stderr on a single self-overwriting line —
///   a live preview of what the model is currently hearing. Sized to the
///   live terminal width via `ioctl(TIOCGWINSZ)` so the line never wraps.
/// - **Committed** transcripts go to stdout, prefixed by the session's tag,
///   on their own line, when our `VadGate` (FluidAudio's Silero VAD) reports a
///   speech-end event. We deliberately do not use FluidAudio's
///   `setEouCallback` — on real microphones with low-level background noise
///   the model keeps emitting tokens through "silence" and the EOU debounce
///   never confirms. A trained VAD is the right tool.
actor ASRSession {
    /// 16 kHz mono Float32 — the format Parakeet's streaming pipeline ingests.
    /// Used to manufacture trailing-silence buffers in `commit()` so the
    /// streaming encoder has the lookahead it needs to decode the last words.
    private static let silenceFormat = AVAudioFormat(
        commonFormat: .pcmFormatFloat32,
        sampleRate: 16_000,
        channels: 1,
        interleaved: false
    )!

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
            let prefix = "[\(tag) …] "
            // Margin of 1 protects against off-by-one issues right at the
            // terminal boundary, where some terminals wrap eagerly.
            let body = max(20, ASRSession.terminalColumns() - prefix.count - 1)
            let display = ASRSession.tailTruncate(trimmed, max: body)
            // \r — return to column 0; \u{1B}[K — clear to end of (current) row.
            let line = "\r\(prefix)\(display)\u{1B}[K"
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
        // Parakeet's streaming encoder holds ~80 ms of audio per chunk in its
        // loopback cache as future-context lookahead — without a next chunk
        // it can't emit tokens for the very last word of an utterance. Push
        // ~500 ms of silence (3 chunks at 160 ms) so the tail decodes before
        // we read out the transcript.
        try await flushTrailingSilence(durationMs: 500)

        let final = try await manager.finish()
        FileHandle.standardError.write(Data("\r\u{1B}[K".utf8))
        let trimmed = final.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty {
            print("[\(tag)] \(trimmed)")
        }
        await manager.reset()
    }

    private func flushTrailingSilence(durationMs: Int) async throws {
        let frames = AVAudioFrameCount(
            ASRSession.silenceFormat.sampleRate * Double(durationMs) / 1000
        )
        guard let buffer = AVAudioPCMBuffer(
            pcmFormat: ASRSession.silenceFormat,
            frameCapacity: frames
        ), let channel = buffer.floatChannelData?[0] else {
            return
        }
        buffer.frameLength = frames
        // Explicitly zero — AVAudioPCMBuffer's backing memory isn't guaranteed
        // to be zero-initialized.
        for i in 0..<Int(frames) {
            channel[i] = 0
        }
        try await manager.appendAudio(buffer)
        try await manager.processBufferedAudio()
    }

    private static func tailTruncate(_ s: String, max: Int) -> String {
        guard s.count > max else { return s }
        let suffix = s.suffix(max - 1)
        return "…" + suffix
    }

    /// Live terminal width via TIOCGWINSZ. Falls back to 120 columns when
    /// stderr isn't a tty (piped, redirected to a file, etc.).
    private static func terminalColumns() -> Int {
        var ws = winsize()
        if ioctl(STDERR_FILENO, TIOCGWINSZ, &ws) == 0, ws.ws_col > 0 {
            return Int(ws.ws_col)
        }
        return 120
    }
}
