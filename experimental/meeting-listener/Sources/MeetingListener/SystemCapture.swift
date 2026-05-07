import AudioToolbox
import AVFoundation
import CoreAudio
import Foundation

/// Captures the system audio mix (everything coming out of the default output
/// device — Zoom, Meet, Slack, music, system sounds) using the Core Audio
/// Process Tap API introduced in macOS 14.2.
///
/// Internally:
///
/// 1. Construct a `CATapDescription` that taps the global output mix and
///    excludes no processes (i.e. captures everything).
/// 2. `AudioHardwareCreateProcessTap` returns an `AudioObjectID` for the tap.
/// 3. Build a private aggregate device that wraps the tap as a sub-tap; the
///    aggregate device is what the IO proc actually reads from.
/// 4. Register an `AudioDeviceIOProcID` block via
///    `AudioDeviceCreateIOProcIDWithBlock` on a dedicated user-initiated queue;
///    each callback receives an `AudioBufferList` of fresh tapped audio.
/// 5. The block copies that audio into an owned `AVAudioPCMBuffer` (the
///    `AudioBufferList` memory is recycled after the callback returns) and
///    yields it to the `AsyncStream`.
///
/// Permission to capture system audio is governed by macOS TCC keyed to the
/// hosting bundle's identifier; the binary needs to be invoked from inside an
/// `.app` bundle whose `Info.plist` has `NSAudioCaptureUsageDescription`.
final class SystemCapture: AudioSource, @unchecked Sendable {
    private let queue = DispatchQueue(label: "SystemCapture.io", qos: .userInitiated)
    private var tapID: AudioObjectID = .unknownObjectID
    private var aggregateDeviceID: AudioObjectID = .unknownObjectID
    private var deviceProcID: AudioDeviceIOProcID?
    private var continuation: AsyncThrowingStream<AVAudioPCMBuffer, Error>.Continuation?

    /// Writes one line to stderr at startup and on the first IO callback so
    /// we can distinguish "tap never created", "tap created but never
    /// produces audio", and "tap producing audio but downstream rejects it".
    private static func diag(_ message: String) {
        FileHandle.standardError.write(Data("[SystemCapture] \(message)\n".utf8))
    }

    func start() throws -> AsyncThrowingStream<AVAudioPCMBuffer, Error> {
        precondition(
            tapID == .unknownObjectID,
            "SystemCapture.start() called while already running"
        )

        // 1. Tap description — global output, no exclusions.
        let tapDescription = CATapDescription(stereoGlobalTapButExcludeProcesses: [])
        tapDescription.uuid = UUID()
        tapDescription.muteBehavior = .unmuted
        tapDescription.isPrivate = true

        var tap = AudioObjectID.unknownObjectID
        let tapStatus = AudioHardwareCreateProcessTap(tapDescription, &tap)
        guard tapStatus == noErr, tap != .unknownObjectID else {
            throw SystemCaptureError.tapCreationFailed(tapStatus)
        }
        self.tapID = tap

        // Anything that throws below this point must clean up the tap so the
        // process doesn't leak Core Audio resources on a failed start().
        do {
            // 2. Read the tap's audio format. Required to wrap incoming
            //    AudioBufferLists in an AVAudioPCMBuffer.
            let format = try Self.readTapFormat(tapID: tap)

            // 3. Build aggregate device that drives the tap.
            let outputDeviceID = try Self.readDefaultSystemOutputDevice()
            let outputUID = try Self.readDeviceUID(deviceID: outputDeviceID)
            let aggregateUID = UUID().uuidString

            let aggDescription: [String: Any] = [
                kAudioAggregateDeviceNameKey: "MeetingListener-Aggregate",
                kAudioAggregateDeviceUIDKey: aggregateUID,
                kAudioAggregateDeviceMainSubDeviceKey: outputUID,
                kAudioAggregateDeviceIsPrivateKey: true,
                kAudioAggregateDeviceIsStackedKey: false,
                kAudioAggregateDeviceTapAutoStartKey: true,
                kAudioAggregateDeviceSubDeviceListKey: [
                    [kAudioSubDeviceUIDKey: outputUID]
                ],
                kAudioAggregateDeviceTapListKey: [
                    [
                        kAudioSubTapDriftCompensationKey: true,
                        kAudioSubTapUIDKey: tapDescription.uuid.uuidString,
                    ]
                ],
            ]

            var device = AudioObjectID.unknownObjectID
            let aggStatus = AudioHardwareCreateAggregateDevice(
                aggDescription as CFDictionary, &device
            )
            guard aggStatus == noErr, device != .unknownObjectID else {
                throw SystemCaptureError.aggregateDeviceCreationFailed(aggStatus)
            }
            self.aggregateDeviceID = device

            // 4. AsyncThrowingStream that the IO proc yields into.
            let (stream, continuation) = AsyncThrowingStream<AVAudioPCMBuffer, Error>.makeStream(
                bufferingPolicy: .bufferingNewest(64)
            )
            self.continuation = continuation

            // 5. Register the IO proc. The block captures the format and the
            //    continuation as locals — it never touches `self` mutable
            //    state, so concurrency between the audio thread and the main
            //    thread is bounded to start/stop sequencing on the device.
            var procID: AudioDeviceIOProcID?
            // `Atomic` would be cleaner but Foundation doesn't ship it on
            // macOS yet; pthread_once via a captured class flag is overkill
            // for "log once". A relaxed dispatch_once via DispatchQueue
            // ensures the diag line fires exactly once even under contention.
            let firstCallback = FirstCallbackFlag()
            let procStatus = AudioDeviceCreateIOProcIDWithBlock(
                &procID,
                device,
                queue
            ) { _, inInputData, _, _, _ in
                if firstCallback.fireOnce() {
                    let frames = inInputData.pointee.mBuffers.mDataByteSize / 4
                    SystemCapture.diag(
                        "first IO callback — frames=\(frames) channels=\(inInputData.pointee.mNumberBuffers)"
                    )
                }
                let owned = SystemCapture.copyOwnedFromBufferList(
                    inInputData,
                    format: format
                )
                continuation.yield(owned)
            }
            guard procStatus == noErr, let procID = procID else {
                throw SystemCaptureError.ioProcCreationFailed(procStatus)
            }
            self.deviceProcID = procID

            // 6. Start the device — IO proc begins firing.
            let startStatus = AudioDeviceStart(device, procID)
            guard startStatus == noErr else {
                throw SystemCaptureError.deviceStartFailed(startStatus)
            }

            Self.diag(
                "started — tap=\(tap) aggregate=\(device) sampleRate=\(format.sampleRate) channels=\(format.channelCount)"
            )
            return stream
        } catch {
            cleanup()
            throw error
        }
    }

    func stop() {
        cleanup()
    }

    /// Tears down the device, IO proc, aggregate, and tap in the order Core
    /// Audio requires. Idempotent — safe to call multiple times and from
    /// `stop()` or the throw path of `start()`.
    private func cleanup() {
        if let procID = deviceProcID, aggregateDeviceID != .unknownObjectID {
            AudioDeviceStop(aggregateDeviceID, procID)
            AudioDeviceDestroyIOProcID(aggregateDeviceID, procID)
            self.deviceProcID = nil
        }
        if aggregateDeviceID != .unknownObjectID {
            AudioHardwareDestroyAggregateDevice(aggregateDeviceID)
            self.aggregateDeviceID = .unknownObjectID
        }
        if tapID != .unknownObjectID {
            AudioHardwareDestroyProcessTap(tapID)
            self.tapID = .unknownObjectID
        }
        continuation?.finish()
        continuation = nil
    }

    // MARK: - Core Audio property helpers

    private static func readTapFormat(tapID: AudioObjectID) throws -> AVAudioFormat {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioTapPropertyFormat,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )
        var size = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
        var description = AudioStreamBasicDescription()
        let status = AudioObjectGetPropertyData(
            tapID, &address, 0, nil, &size, &description
        )
        guard status == noErr else {
            throw SystemCaptureError.readFormatFailed(status)
        }
        guard let format = AVAudioFormat(streamDescription: &description) else {
            throw SystemCaptureError.invalidFormat(description)
        }
        return format
    }

    private static func readDefaultSystemOutputDevice() throws -> AudioObjectID {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioHardwarePropertyDefaultSystemOutputDevice,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )
        var deviceID = AudioObjectID.unknownObjectID
        var size = UInt32(MemoryLayout<AudioObjectID>.size)
        let status = AudioObjectGetPropertyData(
            AudioObjectID(kAudioObjectSystemObject),
            &address, 0, nil, &size, &deviceID
        )
        guard status == noErr, deviceID != .unknownObjectID else {
            throw SystemCaptureError.readDefaultOutputFailed(status)
        }
        return deviceID
    }

    private static func readDeviceUID(deviceID: AudioObjectID) throws -> String {
        var address = AudioObjectPropertyAddress(
            mSelector: kAudioDevicePropertyDeviceUID,
            mScope: kAudioObjectPropertyScopeGlobal,
            mElement: kAudioObjectPropertyElementMain
        )
        var uid: CFString = "" as CFString
        var size = UInt32(MemoryLayout<CFString>.size)
        let status = withUnsafeMutablePointer(to: &uid) { ptr in
            AudioObjectGetPropertyData(deviceID, &address, 0, nil, &size, ptr)
        }
        guard status == noErr else {
            throw SystemCaptureError.readDeviceUIDFailed(status)
        }
        return uid as String
    }

    /// Wraps the borrowed `AudioBufferList` in a temporary no-copy buffer to
    /// read its frame count, allocates a fresh owned buffer of the same size,
    /// and copies the audio over so consumers may hold it across actor hops.
    private static func copyOwnedFromBufferList(
        _ ptr: UnsafePointer<AudioBufferList>,
        format: AVAudioFormat
    ) -> AVAudioPCMBuffer {
        guard let temp = AVAudioPCMBuffer(
            pcmFormat: format,
            bufferListNoCopy: ptr,
            deallocator: nil
        ) else {
            preconditionFailure(
                "AVAudioPCMBuffer(bufferListNoCopy:) failed in system tap IO proc"
            )
        }
        let frameCount = temp.frameLength
        guard let dst = AVAudioPCMBuffer(
            pcmFormat: format,
            frameCapacity: frameCount
        ) else {
            preconditionFailure(
                "AVAudioPCMBuffer allocation failed in system tap IO proc"
            )
        }
        dst.frameLength = frameCount

        let frames = Int(frameCount)
        let channels = Int(format.channelCount)
        guard let srcChannels = temp.floatChannelData,
              let dstChannels = dst.floatChannelData else {
            preconditionFailure(
                "Tap delivered non-Float32 data; expected Float32, got \(format)"
            )
        }
        for ch in 0..<channels {
            memcpy(
                dstChannels[ch],
                srcChannels[ch],
                frames * MemoryLayout<Float>.size
            )
        }
        return dst
    }
}

enum SystemCaptureError: Error, CustomStringConvertible {
    case tapCreationFailed(OSStatus)
    case aggregateDeviceCreationFailed(OSStatus)
    case ioProcCreationFailed(OSStatus)
    case deviceStartFailed(OSStatus)
    case readFormatFailed(OSStatus)
    case readDefaultOutputFailed(OSStatus)
    case readDeviceUIDFailed(OSStatus)
    case invalidFormat(AudioStreamBasicDescription)

    var description: String {
        switch self {
        case .tapCreationFailed(let s):
            return "AudioHardwareCreateProcessTap failed (status \(s)). " +
                "Check the bundle has NSAudioCaptureUsageDescription and TCC permission was granted."
        case .aggregateDeviceCreationFailed(let s):
            return "AudioHardwareCreateAggregateDevice failed (status \(s))."
        case .ioProcCreationFailed(let s):
            return "AudioDeviceCreateIOProcIDWithBlock failed (status \(s))."
        case .deviceStartFailed(let s):
            return "AudioDeviceStart failed (status \(s))."
        case .readFormatFailed(let s):
            return "Failed to read kAudioTapPropertyFormat (status \(s))."
        case .readDefaultOutputFailed(let s):
            return "Failed to read default system output device (status \(s))."
        case .readDeviceUIDFailed(let s):
            return "Failed to read kAudioDevicePropertyDeviceUID (status \(s))."
        case .invalidFormat(let desc):
            return "Tap stream format could not be wrapped in AVAudioFormat: \(desc)"
        }
    }
}

private extension AudioObjectID {
    static let unknownObjectID = AudioObjectID(kAudioObjectUnknown)
}

/// Lock-free single-shot flag, used by the IO proc block to log just the
/// first callback that arrives. The IO proc runs on its own serial dispatch
/// queue, so a plain `Bool` flipped under that queue is race-free against
/// itself; we use the wrapping class so the flag escapes the local block
/// scope by reference.
private final class FirstCallbackFlag: @unchecked Sendable {
    private var fired = false

    func fireOnce() -> Bool {
        guard !fired else { return false }
        fired = true
        return true
    }
}
