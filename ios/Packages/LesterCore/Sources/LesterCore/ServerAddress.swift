import Foundation

public struct ServerAddress: Hashable, Sendable {
    public enum Invalid: Error { case address }
    public let origin: URL
    public let development: Bool

    public init(_ input: String, allowLocalDevelopment: Bool = false) throws {
        guard var parts = URLComponents(string: input.trimmingCharacters(in: .whitespacesAndNewlines)),
              let host = parts.host?.lowercased(), !host.isEmpty,
              parts.user == nil, parts.password == nil, parts.query == nil, parts.fragment == nil,
              ["", "/", "/app", "/app/"].contains(parts.percentEncodedPath),
              parts.port == nil || (1...65535).contains(parts.port!),
              parts.scheme == "https" || allowLocalDevelopment && parts.scheme == "http" && ["localhost", "127.0.0.1", "[::1]", "::1"].contains(host)
        else { throw Invalid.address }
        parts.scheme = parts.scheme?.lowercased(); parts.host = host; parts.path = ""; parts.query = nil
        guard let url = parts.url else { throw Invalid.address }
        origin = url; development = parts.scheme == "http"
    }

    public func url(_ path: String) -> URL { URL(string: path, relativeTo: origin)!.absoluteURL }
    public func sameOrigin(_ url: URL) -> Bool {
        url.scheme?.lowercased() == origin.scheme && url.host?.lowercased() == origin.host?.lowercased()
            && (url.port ?? (url.scheme == "https" ? 443 : 80)) == (origin.port ?? (origin.scheme == "https" ? 443 : 80))
            && url.user == nil && url.password == nil
    }
    public func acceptsBridge(from url: URL, mainFrame: Bool) -> Bool {
        guard mainFrame, sameOrigin(url), url.query == nil || !url.path.hasPrefix("/api/") else { return false }
        return url.path == "/app" || url.path.hasPrefix("/app/") || url.path == "/login"
    }
    public func allowsMicrophone(from source: URL, current: URL, mainFrame: Bool) -> Bool {
        guard mainFrame, sameOrigin(source), sameOrigin(current) else { return false }
        return [source, current].allSatisfy { $0.path == "/app" || $0.path.hasPrefix("/app/") }
    }
    public func allowsNavigation(_ url: URL) -> Bool {
        guard sameOrigin(url) else { return false }
        let path = url.path
        return path == "/" || path == "/login" || path == "/app" || path.hasPrefix("/app/") || path == "/docs" || path.hasPrefix("/docs/") || path.hasPrefix("/preview/")
    }
    public func loginURL(challenge: String, state: String) -> URL {
        var target = URLComponents(); target.path = "/app/native-auth"
        target.queryItems = [.init(name: "challenge", value: challenge), .init(name: "state", value: state)]
        var login = URLComponents(url: url("/login"), resolvingAgainstBaseURL: false)!
        login.queryItems = [.init(name: "returnTo", value: target.string!)]
        return login.url!
    }
    public static func callbackCode(_ callback: URL, state: String) -> String? {
        guard callback.scheme == "lester", callback.host == "auth", callback.path == "/callback",
              callback.user == nil, callback.password == nil, callback.port == nil, callback.fragment == nil,
              let fields = URLComponents(url: callback, resolvingAgainstBaseURL: false)?.queryItems,
              fields.count == 2, fields.filter({ $0.name == "state" }).count == 1,
              fields.first(where: { $0.name == "state" })?.value == state,
              let code = fields.first(where: { $0.name == "code" })?.value,
              code.range(of: "^[A-Za-z0-9_-]{43}$", options: .regularExpression) != nil else { return nil }
        return code
    }
}
