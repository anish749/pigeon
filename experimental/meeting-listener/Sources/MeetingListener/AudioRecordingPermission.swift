import AppKit
import Foundation

/// Checks and requests system-audio recording permission via the private TCC
/// SPIs (`TCCAccessPreflight` / `TCCAccessRequest`). This is the same path
/// AudioCap uses; no public API exists to programmatically request the
/// `kTCCServiceAudioCapture` permission, but `AudioHardwareCreateProcessTap`
/// silently delivers zero-filled buffers when permission is missing — i.e.
/// the bug we hit. We dlopen TCC and call the SPIs ourselves to make the
/// failure mode loud and to actually surface the system prompt.
///
/// Apple discourages SPI use in App Store apps; this is a prototype, so we
/// accept that trade-off.
enum AudioRecordingPermissionStatus: String {
    case authorized
    case denied
    case unknown
}

enum AudioRecordingPermissionError: Error, CustomStringConvertible {
    case tccFrameworkUnavailable
    case symbolMissing(String)
    case denied

    var description: String {
        switch self {
        case .tccFrameworkUnavailable:
            return "Could not load /System/Library/PrivateFrameworks/TCC.framework — system-audio permission cannot be checked."
        case .symbolMissing(let name):
            return "TCC framework loaded but symbol \(name) is missing on this macOS version."
        case .denied:
            return """
                System-audio recording permission denied.
                Open System Settings → Privacy & Security → Screen & System Audio Recording,
                enable MeetingListener, and re-run.
                """
        }
    }
}

private typealias TCCPreflightFn = @convention(c) (CFString, CFDictionary?) -> Int
private typealias TCCRequestFn = @convention(c) (
    CFString, CFDictionary?, @escaping (Bool) -> Void
) -> Void

private enum TCC {
    static let service = "kTCCServiceAudioCapture" as CFString

    static let handle: UnsafeMutableRawPointer? = dlopen(
        "/System/Library/PrivateFrameworks/TCC.framework/Versions/A/TCC",
        RTLD_NOW
    )

    static let preflight: TCCPreflightFn? = {
        guard let handle, let sym = dlsym(handle, "TCCAccessPreflight") else {
            return nil
        }
        return unsafeBitCast(sym, to: TCCPreflightFn.self)
    }()

    static let request: TCCRequestFn? = {
        guard let handle, let sym = dlsym(handle, "TCCAccessRequest") else {
            return nil
        }
        return unsafeBitCast(sym, to: TCCRequestFn.self)
    }()
}

/// Returns the current permission status without prompting.
func checkAudioRecordingPermission() throws -> AudioRecordingPermissionStatus {
    guard TCC.handle != nil else {
        throw AudioRecordingPermissionError.tccFrameworkUnavailable
    }
    guard let preflight = TCC.preflight else {
        throw AudioRecordingPermissionError.symbolMissing("TCCAccessPreflight")
    }
    // Per AudioCap: 0 = authorized, 1 = denied, anything else = unknown.
    switch preflight(TCC.service, nil) {
    case 0: return .authorized
    case 1: return .denied
    default: return .unknown
    }
}

/// Asks macOS to prompt the user for permission. Returns true if the user
/// granted, false if denied. Blocks until the user dismisses the dialog.
func requestAudioRecordingPermission() async throws -> Bool {
    guard TCC.handle != nil else {
        throw AudioRecordingPermissionError.tccFrameworkUnavailable
    }
    guard let request = TCC.request else {
        throw AudioRecordingPermissionError.symbolMissing("TCCAccessRequest")
    }
    return await withCheckedContinuation { (cont: CheckedContinuation<Bool, Never>) in
        request(TCC.service, nil) { granted in
            cont.resume(returning: granted)
        }
    }
}

/// Opens System Settings → Privacy & Security → Screen & System Audio Recording.
/// On macOS 14+ Apple still honors the legacy `Privacy_AudioCapture` URL
/// fragment and routes it to the merged audio/screen pane.
func openAudioRecordingSettings() {
    let url = URL(
        string: "x-apple.systempreferences:com.apple.preference.security?Privacy_AudioCapture"
    )!
    NSWorkspace.shared.open(url)
}

/// Ensures permission is granted before the caller proceeds.
///
/// - If already authorized, returns immediately.
/// - If undetermined, triggers the OS prompt; opens Settings on denial.
/// - If already denied (the user previously said no, or revoked it), opens
///   Settings without re-prompting (the OS will not prompt twice in the same
///   bundle lifetime) and throws so the caller can surface the message and
///   exit cleanly.
func ensureAudioRecordingPermission() async throws {
    let status = try checkAudioRecordingPermission()
    switch status {
    case .authorized:
        return
    case .unknown:
        let granted = try await requestAudioRecordingPermission()
        if granted { return }
        openAudioRecordingSettings()
        throw AudioRecordingPermissionError.denied
    case .denied:
        openAudioRecordingSettings()
        throw AudioRecordingPermissionError.denied
    }
}
