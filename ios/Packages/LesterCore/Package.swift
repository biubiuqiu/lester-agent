// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "LesterCore",
    platforms: [.iOS(.v18), .macOS(.v14)],
    products: [.library(name: "LesterCore", targets: ["LesterCore"])],
    targets: [.target(name: "LesterCore"), .testTarget(name: "LesterCoreTests", dependencies: ["LesterCore"])]
)
