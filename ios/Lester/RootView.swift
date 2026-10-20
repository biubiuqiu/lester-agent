import LesterCore
import SwiftUI

struct RootView: View {
    @Bindable var model: AppModel
    @Environment(\.scenePhase) private var phase
    @State private var address = ""
    @State private var query = ""
    @State private var visibility = NavigationSplitViewVisibility.automatic
    @State private var compactColumn = NavigationSplitViewColumn.detail
    @State private var confirmDisconnect = false
    private var locale: Locale { Locale(identifier: model.workspace?.locale ?? Locale.current.identifier) }

    var body: some View {
        Group {
            if let server = model.server {
                NavigationSplitView(columnVisibility: $visibility, preferredCompactColumn: $compactColumn) {
                    sidebar
                } detail: {
                    ZStack {
                        WorkspaceWebView(model: model, server: server).id(server)
                            .allowsHitTesting(!model.needsSignIn)
                        if model.needsSignIn { signIn }
                        if model.loading && !model.needsSignIn { ProgressView().padding(24).background(.regularMaterial, in: RoundedRectangle(cornerRadius: 18)) }
                    }
                    .navigationTitle("")
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .topBarLeading) {
                            if model.canGoBack { Button("Back", systemImage: "chevron.left") { model.back?() } }
                        }
                        ToolbarItemGroup(placement: .topBarTrailing) {
                            if model.workspace != nil { GlassAction(title: "New task", symbol: "plus") { model.command("home"); compactColumn = .detail } }
                            Button("Settings", systemImage: "slider.horizontal.3") { model.showSettings = true }
                        }
                    }
                    .safeAreaInset(edge: .top, spacing: 0) {
                        if let error = model.error {
                            HStack(alignment: .top) {
                                Image(systemName: "exclamationmark.circle"); Text(LocalizedStringKey(error)).font(.footnote)
                                Spacer(minLength: 8)
                                Button("Retry") { model.needsSignIn ? model.signIn() : model.retry() }.font(.footnote.bold())
                            }.padding(12).background(.regularMaterial).accessibilityElement(children: .contain)
                        }
                    }
                }
                .navigationSplitViewStyle(.balanced)
                .sheet(isPresented: $model.showSettings) { settings }
                .sheet(isPresented: Binding(get: { model.shareURL != nil }, set: { if !$0 { clearDownload() } })) {
                    if let url = model.shareURL { ShareSheet(url: url) }
                }
            } else {
                NavigationStack { connect }
            }
        }
        .environment(\.locale, locale)
        .onChange(of: phase, initial: true) { _, value in model.becameActive(value == .active) }
    }

    private var connect: some View {
        Form {
            Section {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Lester").font(.largeTitle.bold())
                    Text("Make it happen.").font(.title3).foregroundStyle(.secondary)
                }.padding(.vertical, 12)
            }
            Section {
                TextField("https://lester.example", text: $address).textContentType(.URL).keyboardType(.URL)
                    .textInputAutocapitalization(.never).autocorrectionDisabled().accessibilityIdentifier("server-address")
                Button("Connect") { model.connect(address) }.disabled(model.clearingData || address.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    .accessibilityIdentifier("connect-server")
            } header: { Text("Your workspace") } footer: { Text("Connect to your own Lester deployment. Tasks keep running on your server.") }
            if let error = model.error { Section { Text(LocalizedStringKey(error)).foregroundStyle(.red) } }
        }.navigationTitle("Welcome")
    }
    private var signIn: some View {
        VStack(spacing: 18) {
            Image(systemName: "sparkle").font(.system(size: 42)).foregroundStyle(.tint)
            Text("Lester").font(.largeTitle.bold())
            Text("Your work, wherever you are.").foregroundStyle(.secondary)
            if model.signingIn { ProgressView() }
            else { GlassAction(title: "Sign in", symbol: "person.crop.circle") { model.signIn() } }
        }.frame(maxWidth: .infinity, maxHeight: .infinity).background(Color(.systemBackground))
    }
    private var sidebar: some View {
        List {
            Section {
                Button { model.command("home"); compactColumn = .detail } label: { Label("New task", systemImage: "plus") }
            }
            Section("Recent conversations") {
                let items = model.workspace?.conversations.filter { query.isEmpty || $0.title.localizedCaseInsensitiveContains(query) || $0.projectName.localizedCaseInsensitiveContains(query) } ?? []
                if items.isEmpty { Text(LocalizedStringKey(query.isEmpty ? "No conversations yet" : "No matching conversations")).foregroundStyle(.secondary) }
                ForEach(items) { conversation in
                    Button {
                        model.command("conversation", id: conversation.id); compactColumn = .detail
                    } label: {
                        HStack(spacing: 10) {
                            Image(systemName: conversation.runStatus == "running" ? "circle.dotted" : "bubble.left").foregroundStyle(.secondary)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(conversation.title).font(.subheadline).lineLimit(2).foregroundStyle(.primary)
                                Text(conversation.projectName).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                            Spacer(minLength: 0)
                            if model.workspace?.currentID == conversation.id { Image(systemName: "checkmark").font(.caption).foregroundStyle(.tint) }
                        }.padding(.vertical, 3)
                    }.accessibilityIdentifier("conversation-\(conversation.id.uuidString.lowercased())")
                }
            }
        }
        .listStyle(.sidebar).navigationTitle("Lester")
        .searchable(text: $query, prompt: "Search conversations or projects")
        .navigationSplitViewColumnWidth(min: 250, ideal: 300, max: 380)
        .toolbar { ToolbarItem(placement: .bottomBar) { Button("Settings", systemImage: "slider.horizontal.3") { model.showSettings = true } } }
    }
    private var settings: some View {
        NavigationStack {
            Form {
                Section("Workspace") {
                    if let workspace = model.workspace { Text(workspace.displayName) }
                    if let server = model.server { Text(server.origin.absoluteString).font(.footnote).foregroundStyle(.secondary).textSelection(.enabled) }
                    if model.workspace != nil {
                        Button("Account") { model.showSettings = false; model.command("settings"); compactColumn = .detail }
                        Button("Scheduled tasks") { model.showSettings = false; model.command("tasks"); compactColumn = .detail }
                    }
                }
                Section {
                    Toggle("Haptic feedback", isOn: $model.hapticsEnabled)
                } footer: { Text("A gentle response when a task finishes while the app is open. Available on supported hardware.") }
                Section {
                    Button("Change server", role: .destructive) { confirmDisconnect = true }
                } footer: { Text("Changing server clears this device’s sign-in and local drafts. Server tasks and files remain.") }
            }
            .navigationTitle("Settings").navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { model.showSettings = false } } }
            .confirmationDialog("Change server?", isPresented: $confirmDisconnect, titleVisibility: .visible) {
                Button("Change server", role: .destructive) { Task { await model.disconnect() } }
            }
        }.presentationDetents([.medium, .large])
    }
    private func clearDownload() {
        if let url = model.shareURL { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        model.shareURL = nil
    }
}

private struct GlassAction: View {
    let title: LocalizedStringKey
    let symbol: String
    let action: () -> Void
    var body: some View {
        if #available(iOS 26.0, *) {
            Button(action: action) { Label(title, systemImage: symbol) }.buttonStyle(.glass)
        } else {
            Button(action: action) { Label(title, systemImage: symbol) }.buttonStyle(.bordered)
        }
    }
}
private struct ShareSheet: UIViewControllerRepresentable {
    let url: URL
    func makeUIViewController(context: Context) -> UIActivityViewController { UIActivityViewController(activityItems: [url], applicationActivities: nil) }
    func updateUIViewController(_ controller: UIActivityViewController, context: Context) {}
}
