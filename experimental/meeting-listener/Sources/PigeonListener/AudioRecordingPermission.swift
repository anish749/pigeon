import AppKit
import Foundation

/// Checks and requests system-audio recording permission.
///
/// macOS does not expose a public API to programmatically request the
/// `kTCCServiceAudioCapture` permission, but `AudioHardwareCreateProcessTap`
/// silently delivers zero-filled buffers when permission is missing — i.e.
/// "running but transcribing nothing." We use the private TCC SPIs that
/// AudioCap demonstrates (`TCCAccessPreflight` and `TCCAccessRequest`) so
/// the failure mode is loud and the OS prompt actually surfaces.
///
/// The SPI usage is gated behind the compile-time flag `ENABLE_TCC_SPI` so
/// a hypothetical App Store distribution can drop it. When the flag is off,
/// `checkAudioRecordingPermission` reports `.authorized` and
/// `requestAudioRecordingPermission` is a no-op — the caller's code path
/// still works, it just relies on the OS to prompt organically (which on
/// macOS 14.4+ does not happen for direct-exec CLI invocations).
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
                enable Pigeon, and re-run.
                If Pigeon does not appear there, run:
                    make reset-tcc
                then `make run` again.
                """
        }
    }
}

#if ENABLE_TCC_SPI

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

#else // ENABLE_TCC_SPI

func checkAudioRecordingPermission() throws -> AudioRecordingPermissionStatus {
    return .authorized
}

func requestAudioRecordingPermission() async throws -> Bool {
    return true
}

#endif // ENABLE_TCC_SPI

/// Opens System Settings → Privacy & Security → Screen & System Audio Recording.
/// On macOS 14+ the legacy `Privacy_AudioCapture` URL fragment still routes
/// to the merged audio/screen-recording pane.
func openAudioRecordingSettings() {
    let url = URL(
        string: "x-apple.systempreferences:com.apple.preference.security?Privacy_AudioCapture"
    )!
    NSWorkspace.shared.open(url)
}

/// Ensures permission is granted before the caller proceeds.
///
/// - If already authorized, returns.
/// - If undetermined, triggers the OS prompt; opens Settings on denial.
/// - If already denied, opens Settings without re-prompting and throws
///   (the OS will not prompt twice for the same bundle once denied).
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
