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
            // Exclude Views and App from the SwiftPM library target. They depend
            // on each other and on SwiftUI app lifecycle (@main, UIKit delegate)
            // so they only make sense inside the Xcode application target
            // generated from project.yml. SwiftPM is used to run unit tests
            // against the networking/model/utility layers.
            exclude: ["Views", "App"],
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
