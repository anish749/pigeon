import AVFoundation
import Dispatch
import FluidAudio
import Foundation

func warn(_ message: String) {
    FileHandle.standardError.write(Data((message + "\n").utf8))
}

/// One source bound to one ASR session, ready to be driven by `runPipeline`.
struct Pipeline: Sendable {
    let spec: SourceSpec
    let source: any AudioSource
    let session: ASRSession
}

func makeSource(spec: SourceSpec) -> any AudioSource {
    switch spec {
    case .mic: return MicCapture()
    case .system: return SystemCapture()
    case .file(let url): return FileSource(url: url)
    }
}

@MainActor
func loadPipelines(config: Config) async throws -> [Pipeline] {
    var pipelines: [Pipeline] = []
    for spec in config.sources {
        let source = makeSource(spec: spec)
        let session = ASRSession(tag: spec.tag, config: config)
        try await session.loadModels()
        pipelines.append(Pipeline(spec: spec, source: source, session: session))
    }
    return pipelines
}

/// Drains a single source's stream into its session. Exits when the stream
/// ends (Ctrl-C / EOF) or rethrows on stream error.
@Sendable
func runPipeline(source: any AudioSource, session: ASRSession) async throws {
    let stream = try source.start()
    do {
        for try await buffer in stream {
            try await session.append(buffer)
        }
    } catch {
        source.stop()
        throw error
    }
    try await session.finish()
}

@MainActor
func run() async throws {
    let config = Config.parse(Array(CommandLine.arguments.dropFirst()))
    warn("Loading Parakeet + VAD models (first run downloads ~120 MB)...")

    let pipelines = try await loadPipelines(config: config)
    let tags = pipelines.map(\.spec.tag).joined(separator: ", ")
    warn("Models ready. Sources: \(tags). Ctrl-C to stop.")

    // SIGINT closes every source; each pipeline's stream finishes naturally
    // and the for-try-await loops exit. `session.finish()` then flushes any
    // in-flight utterance before runPipeline returns.
    let signalSource = DispatchSource.makeSignalSource(signal: SIGINT, queue: .main)
    signal(SIGINT, SIG_IGN)
    let sources = pipelines.map(\.source)
    signalSource.setEventHandler {
        warn("\nStopping...")
        for source in sources {
            source.stop()
        }
    }
    signalSource.resume()

    try await withThrowingTaskGroup(of: Void.self) { group in
        for pipeline in pipelines {
            group.addTask {
                try await runPipeline(
                    source: pipeline.source,
                    session: pipeline.session
                )
            }
        }
        try await group.waitForAll()
    }
}

do {
    try await run()
} catch {
    warn("fatal: \(error)")
    exit(1)
}
