import AVFoundation
@preconcurrency import CoreML
import Darwin
import FluidAudio
import Foundation

/// Drives a single FluidAudio streaming ASR pipeline and emits transcripts to
/// the console.
///
/// The flow is gated on FluidAudio's Silero VAD. Audio only reaches the
/// Parakeet streaming manager between `speechStart` and `speechEnd` events;
/// during silence we hold the manager idle so its loopback encoder cache
/// can't drift into a degenerate state across long pauses (which manifests
/// as missing words at the start of the next utterance, or whole utterances
/// returning no tokens at all).
///
/// To preserve the ~100 ms of audio that arrives *before* VAD's `speechStart`
/// (Silero needs the probability to cross threshold before reporting speech),
/// we keep a small rolling pre-buffer of the most recent silence-side audio
/// and flush it into the manager on `speechStart`.
///
/// On `speechEnd` we feed an extra ~500 ms of silence so Parakeet's streaming
/// encoder has the future-context lookahead it needs to emit tokens for the
/// last word, then `finish()` and `reset()`.
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

    /// How much pre-`speechStart` audio to retain so the onset isn't clipped.
    private static let preBufferMs: Int = 300

    private let tag: String
    private let manager: StreamingEouAsrManager
    private let vad: VadGate

    private var inSpeech = false
    private var preBuffer: [AVAudioPCMBuffer] = []

    init(tag: String, vadThreshold: Float? = nil) {
        self.tag = tag
        self.manager = StreamingEouAsrManager(configuration: MLModelConfiguration())
        self.vad = VadGate(threshold: vadThreshold)
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
        let events = try await vad.observe(buffer)

        // Order matters: handle speechStart before feeding the current buffer
        // so the pre-buffer flushes first, and handle speechEnd after feeding
        // the buffer so the audio that triggered the end-of-speech detection
        // is decoded before commit.
        for event in events where event == .speechStart {
            try await flushPreBufferIntoManager()
            inSpeech = true
        }

        if inSpeech {
            try await manager.appendAudio(buffer)
            try await manager.processBufferedAudio()
        } else {
            stashInPreBuffer(buffer)
        }

        for event in events where event == .speechEnd {
            inSpeech = false
            try await commit()
        }
    }

    /// Flush whatever is buffered and reset. Call at end-of-stream (e.g. after
    /// reading a file or on Ctrl-C) so a trailing utterance isn't lost.
    func finish() async throws {
        if inSpeech {
            inSpeech = false
            try await commit()
        }
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
        ) else {
            throw ASRSessionError.bufferAllocFailed
        }
        guard let channel = buffer.floatChannelData?[0] else {
            throw ASRSessionError.noFloatChannelData
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

    private func stashInPreBuffer(_ buffer: AVAudioPCMBuffer) {
        preBuffer.append(buffer)
        let sampleRate = buffer.format.sampleRate
        let maxFrames = Int(sampleRate * Double(ASRSession.preBufferMs) / 1000)
        var total = preBuffer.reduce(0) { $0 + Int($1.frameLength) }
        while total > maxFrames, !preBuffer.isEmpty {
            let dropped = preBuffer.removeFirst()
            total -= Int(dropped.frameLength)
        }
    }

    private func flushPreBufferIntoManager() async throws {
        for buffer in preBuffer {
            try await manager.appendAudio(buffer)
        }
        preBuffer.removeAll()
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

enum ASRSessionError: Error, CustomStringConvertible {
    case bufferAllocFailed
    case noFloatChannelData

    var description: String {
        switch self {
        case .bufferAllocFailed:
            return "Failed to allocate AVAudioPCMBuffer for trailing-silence flush."
        case .noFloatChannelData:
            return "AVAudioPCMBuffer for trailing-silence flush has no float channel data."
        }
    }
}
