// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "MeetingListener",
    // Core Audio Process Tap APIs (`AudioHardwareCreateProcessTap`,
    // `CATapDescription`, etc.) require macOS 14.2; we pin to 14.4 because
    // that's when the API stabilized and matches the bundle's
    // `LSMinimumSystemVersion`.
    platforms: [.macOS("14.4")],
    dependencies: [
        .package(url: "https://github.com/FluidInference/FluidAudio.git", from: "0.14.0"),
    ],
    targets: [
        .executableTarget(
            name: "MeetingListener",
            dependencies: [
                .product(name: "FluidAudio", package: "FluidAudio"),
            ],
            // Compile in the private TCC SPIs that programmatically request
            // `kTCCServiceAudioCapture` permission. macOS does not expose a
            // public API for this and the tap silently delivers zero buffers
            // when permission is missing, so without the SPI the failure
            // mode is invisible. A hypothetical App Store distribution can
            // drop this define; the permission code falls back to assuming
            // authorized when the flag is off.
            swiftSettings: [
                .define("ENABLE_TCC_SPI"),
            ]
        ),
    ]
)
