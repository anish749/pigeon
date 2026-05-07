import AVFoundation
import Foundation

/// Plays back an audio file (WAV / AIFF / m4a — anything `AVAudioFile`
/// accepts) into the same `AsyncThrowingStream<AVAudioPCMBuffer>` shape that
/// `MicCapture` and `SystemCapture` use. Lets the test fixture exercise the
/// exact production code path instead of a parallel implementation.
///
/// Playback is as-fast-as-possible: FluidAudio's VAD operates in *audio
/// samples* not wall clock, so chunk pacing doesn't matter — sentence
/// boundaries are recovered from the silence already encoded in the file.
final class FileSource: AudioSource, @unchecked Sendable {
    private let url: URL
    private let chunkFrames: AVAudioFrameCount
    private var continuation: AsyncThrowingStream<AVAudioPCMBuffer, Error>.Continuation?
    private var readTask: Task<Void, Never>?

    init(url: URL, chunkFrames: AVAudioFrameCount = 4096) {
        self.url = url
        self.chunkFrames = chunkFrames
    }

    func start() throws -> AsyncThrowingStream<AVAudioPCMBuffer, Error> {
        precondition(readTask == nil, "FileSource.start() called while already running")

        // Open the file synchronously so a malformed path / unreadable file
        // surfaces as a thrown error from start() rather than as silent
        // termination of an empty stream.
        let file = try AVAudioFile(forReading: url)
        let format = file.processingFormat

        let (stream, continuation) = AsyncThrowingStream<AVAudioPCMBuffer, Error>.makeStream(
            bufferingPolicy: .bufferingNewest(64)
        )
        self.continuation = continuation

        let chunkFrames = self.chunkFrames
        readTask = Task.detached { [weak self] in
            do {
                while file.framePosition < file.length {
                    if Task.isCancelled { break }
                    guard let buffer = AVAudioPCMBuffer(
                        pcmFormat: format,
                        frameCapacity: chunkFrames
                    ) else {
                        throw FileSourceError.bufferAllocFailed
                    }
                    try file.read(into: buffer, frameCount: chunkFrames)
                    if buffer.frameLength == 0 { break }
                    continuation.yield(buffer)
                }
                continuation.finish()
            } catch {
                continuation.finish(throwing: error)
            }
            self?.readTask = nil
        }

        return stream
    }

    func stop() {
        readTask?.cancel()
        readTask = nil
        continuation?.finish()
        continuation = nil
    }
}

enum FileSourceError: Error, CustomStringConvertible {
    case bufferAllocFailed

    var description: String {
        switch self {
        case .bufferAllocFailed:
            return "Failed to allocate AVAudioPCMBuffer for the input file."
        }
    }
}
