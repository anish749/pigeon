import AVFoundation
import FluidAudio
import Foundation

/// Streaming voice-activity gate built on FluidAudio's Silero VAD.
///
/// Used to commit utterances on real silence rather than waiting for the
/// model's own EOU prediction (which doesn't fire reliably under low-level
/// mic noise — fan, HVAC, breath). Each input buffer is resampled to the
/// 16 kHz mono Float32 the VAD model expects, accumulated into 4096-sample
/// chunks (256 ms), and fed to the streaming state machine. The gate
/// surfaces `speechStart` / `speechEnd` events so the caller can gate
/// downstream ASR work on whether speech is currently active.
public enum VadEvent: Sendable {
    case speechStart
    case speechEnd
}

actor VadGate {
    private static let outputFormat = AVAudioFormat(
        commonFormat: .pcmFormatFloat32,
        sampleRate: 16_000,
        channels: 1,
        interleaved: false
    )!

    /// Probability above which the streaming VAD fires `speechStart`.
    /// Owned by `Config` — no fallback default here.
    private let threshold: Float
    private var manager: VadManager?
    private var state: VadStreamState = .initial()
    private var pending: [Float] = []
    private var converter: AVAudioConverter?

    init(threshold: Float) {
        self.threshold = threshold
    }

    func loadModel() async throws {
        let config = VadConfig(defaultThreshold: threshold)
        let m = try await VadManager(config: config)
        self.manager = m
        self.state = await m.makeStreamState()
    }

    /// Returns the events the streaming VAD emitted while consuming this
    /// buffer (in order). Most calls return an empty array; transitions are
    /// rare relative to chunk frequency.
    func observe(_ buffer: AVAudioPCMBuffer) async throws -> [VadEvent] {
        guard let manager = manager else {
            throw VadGateError.modelNotLoaded
        }

        let conv = try ensureConverter(for: buffer.format)
        let resampled = try resample(buffer, with: conv)
        pending.append(contentsOf: resampled)

        var events: [VadEvent] = []
        while pending.count >= VadManager.chunkSize {
            let chunk = Array(pending.prefix(VadManager.chunkSize))
            pending.removeFirst(VadManager.chunkSize)
            let result = try await manager.processStreamingChunk(chunk, state: state)
            state = result.state
            switch result.event?.kind {
            case .speechStart: events.append(.speechStart)
            case .speechEnd: events.append(.speechEnd)
            case nil: break
            }
        }
        return events
    }

    private func ensureConverter(for inputFormat: AVAudioFormat) throws -> AVAudioConverter {
        if let existing = converter, existing.inputFormat == inputFormat {
            return existing
        }
        guard let conv = AVAudioConverter(from: inputFormat, to: VadGate.outputFormat) else {
            throw VadGateError.converterCreateFailed
        }
        converter = conv
        return conv
    }

    private func resample(
        _ input: AVAudioPCMBuffer,
        with conv: AVAudioConverter
    ) throws -> [Float] {
        let ratio = VadGate.outputFormat.sampleRate / input.format.sampleRate
        let outputCapacity = AVAudioFrameCount(Double(input.frameLength) * ratio + 64)
        guard let output = AVAudioPCMBuffer(
            pcmFormat: VadGate.outputFormat,
            frameCapacity: outputCapacity
        ) else {
            throw VadGateError.bufferAllocFailed
        }

        var error: NSError?
        var consumed = false
        let status = conv.convert(to: output, error: &error) { _, statusPtr in
            if consumed {
                statusPtr.pointee = .noDataNow
                return nil
            }
            consumed = true
            statusPtr.pointee = .haveData
            return input
        }
        if let error = error { throw VadGateError.convertFailed(underlying: error) }
        if status == .error { throw VadGateError.convertFailed(underlying: nil) }
        guard let channel = output.floatChannelData?[0] else {
            throw VadGateError.noOutputData
        }
        return Array(UnsafeBufferPointer(start: channel, count: Int(output.frameLength)))
    }
}

enum VadGateError: Error, CustomStringConvertible {
    case modelNotLoaded
    case converterCreateFailed
    case bufferAllocFailed
    case convertFailed(underlying: NSError?)
    case noOutputData

    var description: String {
        switch self {
        case .modelNotLoaded:
            return "VadGate.observe() called before loadModel() succeeded."
        case .converterCreateFailed:
            return "Failed to create AVAudioConverter for VAD."
        case .bufferAllocFailed:
            return "Failed to allocate AVAudioPCMBuffer for VAD."
        case .convertFailed(let underlying):
            if let underlying = underlying {
                return "AVAudioConverter failed during VAD resample: \(underlying)"
            }
            return "AVAudioConverter reported an error during VAD resampling."
        case .noOutputData:
            return "VAD resample produced no float channel data."
        }
    }
}
