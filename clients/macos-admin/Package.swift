// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "AgentagotchiAdmin",
    platforms: [
        .macOS(.v14),
    ],
    products: [
        .library(name: "AdminClient", targets: ["AdminClient"]),
        .executable(name: "AgentagotchiAdmin", targets: ["AgentagotchiAdmin"]),
    ],
    targets: [
        .target(
            name: "AdminClient",
            path: "Sources/AdminClient"
        ),
        .executableTarget(
            name: "AgentagotchiAdmin",
            dependencies: ["AdminClient"],
            path: "Sources/AgentagotchiAdmin"
        ),
        .testTarget(
            name: "AdminClientTests",
            dependencies: ["AdminClient"],
            path: "Tests/AdminClientTests"
        ),
    ]
)
