import Foundation

public struct NativeConversation: Decodable, Identifiable, Sendable {
    public let id: UUID
    public let title: String
    public let projectID: UUID
    public let projectName: String
    public let updatedAt: String
    public let runID: UUID?
    public let runStatus: String
    enum CodingKeys: String, CodingKey { case id, title; case projectID = "project_id", projectName = "project_name", updatedAt = "updated_at", runID = "run_id", runStatus = "run_status" }
}
public struct NativeProject: Decodable, Identifiable, Sendable { public let id: UUID; public let name: String }
public struct WorkspaceSnapshot: Decodable, Sendable {
    public let userID: UUID
    public let workspaceID: UUID
    public let displayName: String
    public let currentID: UUID?
    public let locale: String
    public let projects: [NativeProject]
    public let conversations: [NativeConversation]
    enum CodingKeys: String, CodingKey {
        case userID = "user_id", workspaceID = "workspace_id", displayName = "display_name", currentID = "current_id"
        case locale, projects, conversations
    }
    public var account: String { "\(userID.uuidString.lowercased()):\(workspaceID.uuidString.lowercased())" }
    public var valid: Bool {
        projects.count <= 100 && conversations.count <= 200 && displayName.utf8.count <= 512
            && ["zh-CN", "en", "ja", "ko", "fr", "es"].contains(locale)
            && Set(projects.map(\.id)).count == projects.count && Set(conversations.map(\.id)).count == conversations.count
            && projects.allSatisfy { $0.name.utf8.count <= 512 }
            && conversations.allSatisfy { $0.title.utf8.count <= 2048 && $0.projectName.utf8.count <= 512 }
    }
    public init(data: Data) throws { self = try JSONDecoder().decode(Self.self, from: data) }
}
public struct RunCompletion: Decodable, Sendable {
    public let account: String
    public let runID: UUID
    public let conversationID: UUID
    public let eventID: Int64
    enum CodingKeys: String, CodingKey { case account; case runID = "run_id", conversationID = "conversation_id", eventID = "event_id" }
    public init(data: Data) throws { self = try JSONDecoder().decode(Self.self, from: data) }
}

// Foreground-only, once per run. Old transcript/outbox replay and app activation
// must not feel like a newly finished task. Persist only bounded receipt IDs.
public struct CompletionFeedbackGate: Sendable {
    private var account = ""
    private var active: [UUID: UUID] = [:]
    public private(set) var delivered: [String]
    public init(delivered: [String] = []) { self.delivered = Array(delivered.suffix(256)) }
    public mutating func observe(_ snapshot: WorkspaceSnapshot) {
        if account != snapshot.account { account = snapshot.account; active.removeAll() }
        active = Dictionary( snapshot.conversations.compactMap { item in
            guard let run = item.runID, ["running", "cancelling"].contains(item.runStatus), !delivered.contains(receipt(run)) else { return nil }
            return (run, item.id)
        }, uniquingKeysWith: { first, _ in first })
    }
    private func receipt(_ run: UUID) -> String { "\(account):\(run.uuidString.lowercased())" }
    public mutating func complete(_ event: RunCompletion, foreground: Bool) -> Bool {
        guard event.account == account, event.eventID > 0, active.removeValue(forKey: event.runID) == event.conversationID else { return false }
        let key = receipt(event.runID)
        guard !delivered.contains(key) else { return false }
        delivered = Array((delivered + [key]).suffix(256))
        return foreground
    }
    public mutating func reset() { active.removeAll(); account = "" }
}
