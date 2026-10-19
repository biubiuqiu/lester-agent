import AuthenticationServices
import CryptoKit
import LesterCore
import Observation
import Security
import SwiftUI
import WebKit

@MainActor @Observable final class AppModel: NSObject {
    var server: ServerAddress?
    var workspace: WorkspaceSnapshot?
    var loading = true
    var needsSignIn = false
    var error: String?
    var clearingData = false
    var signingIn = false
    var showSettings = false
    var shareURL: URL?
    var foreground = false
    var hapticsEnabled: Bool {
        didSet { UserDefaults.standard.set(hapticsEnabled, forKey: "lester.haptics") }
    }
    var navigate: (([String: String]) -> Void)?
    var load: ((URL) -> Void)?
    var back: (() -> Void)?
    var canGoBack = false
    private var authentication: ASWebAuthenticationSession?
    private var feedback = CompletionFeedbackGate()
    private let store = WKWebsiteDataStore.default()
    var websiteDataStore: WKWebsiteDataStore { store }

    override init() {
        hapticsEnabled = UserDefaults.standard.object(forKey: "lester.haptics") as? Bool ?? true
        super.init()
        if let saved = UserDefaults.standard.string(forKey: "lester.server") { server = try? Self.address(saved) }
        if let server { feedback = CompletionFeedbackGate(delivered: UserDefaults.standard.stringArray(forKey: receiptKey(server)) ?? []) }
    }
    static func address(_ text: String) throws -> ServerAddress {
        #if DEBUG
        return try ServerAddress(text, allowLocalDevelopment: true)
        #else
        return try ServerAddress(text)
        #endif
    }
    private func receiptKey(_ server: ServerAddress) -> String {
        "lester.feedback." + SHA256.hash(data: Data(server.origin.absoluteString.utf8)).map { String(format: "%02x", $0) }.joined()
    }
    func connect(_ input: String) {
        guard !clearingData else { return }
        do {
            let address = try Self.address(input)
            server = address; UserDefaults.standard.set(address.origin.absoluteString, forKey: "lester.server")
            workspace = nil; loading = true; error = nil; needsSignIn = false
            feedback = CompletionFeedbackGate(delivered: UserDefaults.standard.stringArray(forKey: receiptKey(address)) ?? [])
        } catch { self.error = "Use an HTTPS server address, such as https://lester.example." }
    }
    func receive(type: String, payload: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: payload), data.count <= 512_000 else { return }
        switch type {
        case "workspace":
            guard let snapshot = try? WorkspaceSnapshot(data: data), snapshot.valid else { return }
            workspace = snapshot; feedback.observe(snapshot); loading = false; needsSignIn = false; error = nil
        case "signed_out":
            workspace = nil; feedback.reset(); needsSignIn = true; loading = false
        case "run_completed":
            guard let completion = try? RunCompletion(data: data), workspace?.account == completion.account else { return }
            if feedback.complete(completion, foreground: foreground && UIApplication.shared.applicationState == .active), hapticsEnabled { Haptics.completed() }
            if let server { UserDefaults.standard.set(feedback.delivered, forKey: receiptKey(server)) }
        case "route":
            loading = false
        default: break
        }
    }
    func command(_ action: String, id: UUID? = nil) {
        var value = ["action": action]; if let id { value["id"] = id.uuidString.lowercased() }
        navigate?(value)
        if hapticsEnabled { Haptics.selection() }
    }
    func becameActive(_ value: Bool) {
        foreground = value
        if !value { feedback.reset() }
        if value { navigate?(["action": "foreground"]) }
    }
    func retry() { guard let server else { return }; error = nil; loading = true; load?(server.url("/app")) }
    func disconnect() async {
        clearingData = true; server = nil
        if let shareURL { try? FileManager.default.removeItem(at: shareURL.deletingLastPathComponent()) }; shareURL = nil
        authentication?.cancel(); authentication = nil
        workspace = nil; feedback.reset(); navigate = nil; load = nil; back = nil
        let types = WKWebsiteDataStore.allWebsiteDataTypes()
        await store.removeData(ofTypes: types, modifiedSince: .distantPast)
        server = nil; needsSignIn = false; loading = true; error = nil; showSettings = false
        UserDefaults.standard.removeObject(forKey: "lester.server")
        clearingData = false
    }
    func signIn() {
        guard let server, !signingIn else { return }
        do {
            let verifier = try Self.random(), state = try Self.random()
            let challenge = Data(SHA256.hash(data: Data(verifier.utf8))).base64URL
            signingIn = true; error = nil
            let session = ASWebAuthenticationSession(url: server.loginURL(challenge: challenge, state: state), callbackURLScheme: "lester") { [weak self] callback, failure in
                Task { @MainActor in
                    guard let self else { return }
                    defer { self.signingIn = false; self.authentication = nil }
                    guard self.server == server else { return }
                    guard let callback else {
                        if (failure as? ASWebAuthenticationSessionError)?.code != .canceledLogin { self.error = "Sign-in did not finish. Please try again." }
                        return
                    }
                    guard let code = ServerAddress.callbackCode(callback, state: state) else { self.error = "Sign-in did not finish. Please try again."; return }
                    do {
                        let cookies = try await Self.exchange(server: server, code: code, verifier: verifier)
                        guard self.server == server else { return }
                        for cookie in cookies { await self.store.httpCookieStore.setCookie(cookie) }
                        self.needsSignIn = false; self.loading = true; self.load?(server.url("/app"))
                    } catch { self.error = "Sign-in did not finish. Please try again." }
                }
            }
            session.presentationContextProvider = self
            authentication = session
            if !session.start() { authentication = nil; signingIn = false; error = "Sign-in did not finish. Please try again." }
        } catch { self.error = "Sign-in did not finish. Please try again." }
    }
    private static func random() throws -> String {
        var bytes = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else { throw URLError(.unknown) }
        return Data(bytes).base64URL
    }
    private static func exchange(server: ServerAddress, code: String, verifier: String) async throws -> [HTTPCookie] {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.httpShouldSetCookies = false; configuration.httpCookieStorage = nil; configuration.urlCache = nil
        let session = URLSession(configuration: configuration, delegate: NoRedirects(), delegateQueue: nil)
        defer { session.invalidateAndCancel() }
        var request = URLRequest(url: server.url("/api/v1/auth/native/exchange"))
        request.httpMethod = "POST"; request.timeoutInterval = 20
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue(server.origin.absoluteString, forHTTPHeaderField: "Origin")
        request.httpBody = try JSONSerialization.data(withJSONObject: ["code": code, "verifier": verifier])
        let (_, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse, http.statusCode == 204, http.url.map(server.sameOrigin) == true else { throw URLError(.userAuthenticationRequired) }
        let fields = http.allHeaderFields.reduce(into: [String: String]()) { result, value in if let key = value.key as? String, let text = value.value as? String { result[key] = text } }
        let cookies = HTTPCookie.cookies(withResponseHeaderFields: fields, for: request.url!).filter { cookie in
            let paths = ["lester_access_token": "/", "lester_refresh_token": "/api/v1/auth"]
            return paths[cookie.name] == cookie.path && cookie.domain.lowercased() == server.origin.host?.lowercased()
                && cookie.isHTTPOnly && (server.development || cookie.isSecure)
        }
        guard Set(cookies.map(\.name)) == ["lester_access_token", "lester_refresh_token"] else { throw URLError(.cannotParseResponse) }
        return cookies
    }
}

extension AppModel: ASWebAuthenticationPresentationContextProviding {
    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.flatMap(\.windows).first(where: \.isKeyWindow) ?? ASPresentationAnchor()
    }
}
private final class NoRedirects: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) { completionHandler(nil) }
}
private extension Data {
    var base64URL: String { base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "") }
}
