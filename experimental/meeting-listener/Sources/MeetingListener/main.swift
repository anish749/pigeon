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
func runMic(session: ASRSession) async {
    warn("Model ready. Starting microphone capture (Ctrl-C to stop).")

    let mic = MicCapture { buffer in
        Task {
            do {
                try await session.append(buffer)
            } catch {
                warn("append error: \(error)")
            }
        }
    }

    do {
        try mic.start()
    } catch {
        warn("Failed to start mic: \(error)")
        exit(1)
    }

    // Block on SIGINT, then flush before exit.
    let signalSource = DispatchSource.makeSignalSource(signal: SIGINT, queue: .main)
    signal(SIGINT, SIG_IGN)

    await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
        signalSource.setEventHandler {
            continuation.resume()
        }
        signalSource.resume()
    }

    warn("\nStopping...")
    mic.stop()
    do {
        try await session.finish()
    } catch {
        warn("finish error: \(error)")
    }
}

@MainActor
func runFile(path: String, session: ASRSession) async {
    let url = URL(fileURLWithPath: path)
    warn("Replaying \(url.path) through ASR session...")
    let source = FileSource(url: url)
    do {
        try await source.play { buffer in
            try await session.append(buffer)
        }
        try await session.finish()
    } catch {
        warn("File replay failed: \(error)")
        exit(1)
    }
}

@MainActor
func run() async {
    warn("Loading Parakeet model (first run downloads ~120 MB)...")

    let session = ASRSession(tag: "MIC")
    do {
        try await session.loadModels()
    } catch {
        warn("Failed to load model: \(error)")
        exit(1)
    }

    if let filePath = argValue("file") {
        await runFile(path: filePath, session: session)
    } else {
        await runMic(session: session)
    }
}

await run()
