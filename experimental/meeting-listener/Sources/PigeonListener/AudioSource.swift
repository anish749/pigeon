import AVFoundation

/// Common shape for anything that produces audio buffers we can feed into the
/// ASR pipeline. Mic capture, system-audio tap, and file replay all conform —
/// the orchestrator treats them uniformly and only the obtain-audio half of
/// each pipeline differs.
///
/// Conformances must:
///
/// - Throw on setup failure from `start()` so the caller's top-level
///   supervisor sees the error rather than getting a stream that never
///   produces buffers.
/// - Drop on overflow rather than queue unboundedly inside the stream's
///   continuation; ASR slower than realtime should degrade gracefully, not
///   OOM.
/// - Emit buffers the consumer can hold across actor hops, i.e. each yielded
///   `AVAudioPCMBuffer` must own its own audio memory (no
///   `bufferListNoCopy:` references to recycled Core Audio backing).
/// - Make `stop()` idempotent and safe from any context — the SIGINT handler
///   calls it from the main dispatch queue.
protocol AudioSource: Sendable {
    func start() throws -> AsyncThrowingStream<AVAudioPCMBuffer, Error>
    func stop()
}
