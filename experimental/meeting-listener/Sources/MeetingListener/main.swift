import AVFoundation
import Dispatch
import FluidAudio
import Foundation

func warn(_ message: String) {
    FileHandle.standardError.write(Data((message + "\n").utf8))
}

/// Returns the value following `--<flag>` on the command line, or nil.
func argValue(_ flag: String) -> String? {
    let args = CommandLine.arguments
    guard let idx = args.firstIndex(of: "--" + flag), idx + 1 < args.count else {
        return nil
    }
    return args[idx + 1]
}

@MainActor
func runMic(session: ASRSession) async throws {
    warn("Model ready. Starting microphone capture (Ctrl-C to stop).")

    let mic = MicCapture()
    let stream = try mic.start()

    // SIGINT closes the audio stream; the for-await loop below exits naturally
    // and runMic continues to flush a final transcript before returning.
    let signalSource = DispatchSource.makeSignalSource(signal: SIGINT, queue: .main)
    signal(SIGINT, SIG_IGN)
    signalSource.setEventHandler {
        warn("\nStopping...")
        mic.stop()
    }
    signalSource.resume()

    do {
        for await buffer in stream {
            try await session.append(buffer)
        }
    } catch {
        // Stop the engine before re-throwing so we don't leave the audio
        // hardware running on the way out.
        mic.stop()
        throw error
    }

    try await session.finish()
}

@MainActor
func runFile(path: String, session: ASRSession) async throws {
    let url = URL(fileURLWithPath: path)
    warn("Replaying \(url.path) through ASR session...")
    let source = FileSource(url: url)
    try await source.play { buffer in
        try await session.append(buffer)
    }
    try await session.finish()
}

@MainActor
func run() async throws {
    warn("Loading Parakeet + VAD models (first run downloads ~120 MB)...")

    let session = ASRSession(
        tag: "MIC",
        vadThreshold: argValue("vad-threshold").flatMap(Float.init)
    )
    try await session.loadModels()

    if let filePath = argValue("file") {
        try await runFile(path: filePath, session: session)
    } else {
        try await runMic(session: session)
    }
}

do {
    try await run()
} catch {
    warn("fatal: \(error)")
    exit(1)
}
