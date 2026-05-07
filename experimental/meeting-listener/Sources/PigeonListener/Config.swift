import Foundation

/// Single source of truth for all tunable defaults.
///
/// Components (`VadGate`, `ASRSession`, `MicCapture`, `SystemCapture`,
/// `FileSource`) take their values from a `Config` and never carry their own
/// fallback defaults. CLI flags override fields on a `Config` value; nothing
/// else.
struct Config: Sendable {
    /// Probability threshold above which Silero VAD reports `speechStart`.
    /// 0.5 catches short utterances ("yes"/"okay"); raise toward 0.85 if the
    /// environment is noisy enough that 0.5 produces false positives.
    var vadThreshold: Float = 0.5

    /// How much pre-`speechStart` audio is retained inside `ASRSession` so
    /// the first phoneme isn't clipped (Silero needs probability to cross
    /// threshold before reporting, which lags ~100 ms behind real onset).
    var preBufferMs: Int = 300

    /// How much silence is fed to Parakeet's streaming encoder before
    /// `finish()` so its loopback lookahead can resolve the last word.
    var trailingSilenceMs: Int = 500

    /// What the binary should listen to. Default: mic + system in parallel —
    /// both halves of the meeting. `--file <path>` replaces this with a
    /// single-source replay for deterministic testing.
    var sources: [SourceSpec] = [.mic, .system]

    /// Parses CLI args into a `Config`. Unknown flags are ignored (we don't
    /// accept any beyond what's listed here).
    static func parse(_ args: [String]) -> Config {
        var c = Config()
        if let v = value("--vad-threshold", in: args).flatMap(Float.init) {
            c.vadThreshold = v
        }
        if let v = value("--pre-buffer-ms", in: args).flatMap(Int.init) {
            c.preBufferMs = v
        }
        if let v = value("--trailing-silence-ms", in: args).flatMap(Int.init) {
            c.trailingSilenceMs = v
        }
        if let path = value("--file", in: args) {
            c.sources = [.file(URL(fileURLWithPath: path))]
        }
        return c
    }

    private static func value(_ flag: String, in args: [String]) -> String? {
        guard let idx = args.firstIndex(of: flag), idx + 1 < args.count else {
            return nil
        }
        return args[idx + 1]
    }
}

/// Declarative description of a source to run. The orchestrator turns each
/// spec into a concrete `AudioSource` and an `ASRSession` tagged accordingly.
enum SourceSpec: Sendable, Equatable {
    case mic
    case system
    case file(URL)

    /// Human-readable tag printed alongside committed transcripts.
    var tag: String {
        switch self {
        case .mic: return "MIC"
        case .system: return "SYS"
        case .file: return "FILE"
        }
    }
}
