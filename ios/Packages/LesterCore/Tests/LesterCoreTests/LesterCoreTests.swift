import Foundation
import XCTest
@testable import LesterCore

final class LesterCoreTests: XCTestCase {
    func testAddressAndOriginBoundaries() throws {
        let server = try ServerAddress(" https://lester.example:443/app ")
        XCTAssertEqual(server.url("/app").absoluteString, "https://lester.example:443/app")
        XCTAssertTrue(server.sameOrigin(URL(string: "https://lester.example/app/c/123")!))
        XCTAssertFalse(server.sameOrigin(URL(string: "https://lester.example.evil.test/app")!))
        XCTAssertFalse(server.sameOrigin(URL(string: "https://lester.example:444/app")!))
        XCTAssertFalse(server.sameOrigin(URL(string: "https://user@lester.example/app")!))
        for input in ["http://lester.example", "https://user:password@lester.example", "https://lester.example/path", "https://lester.example?token=private", "file:///app", "javascript:alert(1)", "https://lester.example/#token"] {
            XCTAssertThrowsError(try ServerAddress(input))
        }
        XCTAssertThrowsError(try ServerAddress("http://localhost:13020"))
        XCTAssertNoThrow(try ServerAddress("http://localhost:13020", allowLocalDevelopment: true))
        XCTAssertThrowsError(try ServerAddress("http://example.com", allowLocalDevelopment: true))
    }
    func testGeneratedPreviewsAndSubframesNeverGainNativeBridge() throws {
        let server = try ServerAddress("https://lester.example")
        XCTAssertTrue(server.acceptsBridge(from: server.url("/app"), mainFrame: true))
        XCTAssertTrue(server.acceptsBridge(from: server.url("/app/c/123"), mainFrame: true))
        XCTAssertFalse(server.acceptsBridge(from: server.url("/app"), mainFrame: false))
        for path in ["/preview/123", "/api/v1/conversations", "/docs", "/app-evil", "/"] {
            XCTAssertFalse(server.acceptsBridge(from: server.url(path), mainFrame: true))
        }
        XCTAssertTrue(server.allowsNavigation(server.url("/preview/123")))
        XCTAssertFalse(server.allowsNavigation(URL(string: "https://external.example/app")!))
    }
    func testSignInCallbackBindsStateAndSingleCode() throws {
        let server = try ServerAddress("https://lester.example"), state = String(repeating: "s", count: 43), code = String(repeating: "c", count: 43)
        let url = server.loginURL(challenge: String(repeating: "a", count: 43), state: state)
        XCTAssertTrue(server.sameOrigin(url))
        let target = URLComponents(url: url, resolvingAgainstBaseURL: false)!.queryItems!.first!.value!
        XCTAssertTrue(target.hasPrefix("/app/native-auth?challenge="))
        XCTAssertEqual(ServerAddress.callbackCode(URL(string: "lester://auth/callback?code=\(code)&state=\(state)")!, state: state), code)
        for input in ["lester://auth:443/callback?code=\(code)&state=\(state)", "lester://auth/callback?code=\(code)&state=wrong", "https://auth/callback?code=\(code)&state=\(state)", "lester://evil/callback?code=\(code)&state=\(state)", "lester://auth/callback?code=\(code)&code=\(code)&state=\(state)", "lester://auth/callback?code=\(code)&state=\(state)#secret"] {
            XCTAssertNil(ServerAddress.callbackCode(URL(string: input)!, state: state))
        }
    }
    func testMicrophoneIsLimitedToTheCurrentMainWorkspaceFrame() throws {
        let server = try ServerAddress("https://lester.example")
        let app = server.url("/app/c/123")
        XCTAssertTrue(server.allowsMicrophone(from: app, current: app, mainFrame: true))
        XCTAssertFalse(server.allowsMicrophone(from: app, current: app, mainFrame: false))
        for path in ["/preview/123", "/login", "/docs", "/api/v1/me", "/app-evil", "/"] {
            let other = server.url(path)
            XCTAssertFalse(server.allowsMicrophone(from: other, current: app, mainFrame: true))
            XCTAssertFalse(server.allowsMicrophone(from: app, current: other, mainFrame: true))
        }
        for url in ["https://evil.example/app", "https://lester.example:444/app", "https://user@lester.example/app", "about:blank", "blob:https://lester.example/id"] {
            XCTAssertFalse(server.allowsMicrophone(from: URL(string: url)!, current: app, mainFrame: true))
        }
    }
    private let account = "11111111-1111-4111-8111-111111111111:22222222-2222-4222-8222-222222222222"
    private let run = "33333333-3333-4333-8333-333333333333"
    private let conversation = "44444444-4444-4444-8444-444444444444"
    private func snapshot(status: String = "running") throws -> WorkspaceSnapshot {
        try WorkspaceSnapshot(data: Data("""
        {"user_id":"11111111-1111-4111-8111-111111111111","workspace_id":"22222222-2222-4222-8222-222222222222","display_name":"User","current_id":null,"locale":"en","projects":[],"conversations":[{"id":"\(conversation)","title":"Review","project_id":"55555555-5555-4555-8555-555555555555","project_name":"Product","updated_at":"2026-10-19T00:00:00Z","run_id":"\(run)","run_status":"\(status)"}]}
        """.utf8))
    }
    private func completion(account: String? = nil) throws -> RunCompletion {
        try RunCompletion(data: Data("""
        {"account":"\(account ?? self.account)","run_id":"\(run)","conversation_id":"\(conversation)","event_id":12}
        """.utf8))
    }
    func testFeedbackRequiresObservedActiveTaskAndIsDeliveredOnce() throws {
        var gate = CompletionFeedbackGate()
        XCTAssertFalse(gate.complete(try completion(), foreground: true))
        let state = try snapshot(); XCTAssertTrue(state.valid); gate.observe(state)
        XCTAssertFalse(gate.complete(try completion(account: "other-account"), foreground: true))
        XCTAssertTrue(gate.complete(try completion(), foreground: true))
        gate.observe(state)
        XCTAssertFalse(gate.complete(try completion(), foreground: true))
        var reopened = CompletionFeedbackGate(delivered: gate.delivered); reopened.observe(state)
        XCTAssertFalse(reopened.complete(try completion(), foreground: true))
    }
    func testBackgroundAndHistoricalCompletionDoNotVibrateOnReturn() throws {
        var gate = CompletionFeedbackGate(); gate.observe(try snapshot(status: "completed"))
        XCTAssertFalse(gate.complete(try completion(), foreground: true))
        gate.observe(try snapshot()); XCTAssertFalse(gate.complete(try completion(), foreground: false))
        gate.observe(try snapshot()); XCTAssertFalse(gate.complete(try completion(), foreground: true))
        gate.reset(); XCTAssertFalse(gate.complete(try completion(), foreground: true))
    }
}
