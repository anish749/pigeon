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
            ]
        ),
    ]
)
