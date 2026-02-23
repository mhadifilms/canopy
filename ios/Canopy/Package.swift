// swift-tools-version: 5.10

import PackageDescription

let package = Package(
    name: "Canopy",
    platforms: [
        .iOS(.v17),
        .macOS(.v14),
    ],
    products: [
        .library(name: "CanopyKit", targets: ["CanopyKit"]),
    ],
    targets: [
        .target(
            name: "CanopyKit",
            path: "Sources",
            exclude: ["Views"],
            swiftSettings: [
                .enableExperimentalFeature("StrictConcurrency"),
            ]
        ),
        .testTarget(
            name: "CanopyKitTests",
            dependencies: ["CanopyKit"],
            path: "Tests"
        ),
    ]
)
