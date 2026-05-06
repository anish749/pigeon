import AVFoundation
import Foundation

/// Reads an audio file (WAV / AIFF / m4a — anything `AVAudioFile` accepts) and
/// invokes the supplied async callback once per chunk.
///
/// Used by the `--file <path>` mode to drive the same `ASRSession` deterministically
/// from a fixture instead of the live microphone. Playback is as-fast-as-possible:
/// FluidAudio's EOU debounce is measured in *audio samples* at 16 kHz, not wall
/// clock, so chunk pacing doesn't matter — sentence boundaries are recovered
/// from the silence already present in the file.
final class FileSource {
    let url: URL
    private let chunkFrames: AVAudioFrameCount

    init(url: URL, chunkFrames: AVAudioFrameCount = 4096) {
        self.url = url
        self.chunkFrames = chunkFrames
    }

    func play(onBuffer: (AVAudioPCMBuffer) async throws -> Void) async throws {
        let file = try AVAudioFile(forReading: url)
        let format = file.processingFormat

        while file.framePosition < file.length {
            guard let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: chunkFrames) else {
                throw FileSourceError.bufferAllocFailed
            }
            try file.read(into: buffer, frameCount: chunkFrames)
            if buffer.frameLength == 0 { break }
            try await onBuffer(buffer)
        }
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
