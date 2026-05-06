// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "MeetingListener",
    platforms: [.macOS(.v14)],
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
