import LesterCore
import SwiftUI
import WebKit

struct WorkspaceWebView: UIViewRepresentable {
    let model: AppModel
    let server: ServerAddress
    func makeCoordinator() -> Coordinator { Coordinator(model: model, server: server) }
    func makeUIView(context: Context) -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = model.websiteDataStore
        configuration.preferences.javaScriptCanOpenWindowsAutomatically = false
        configuration.userContentController.add(context.coordinator, name: "lester")
        configuration.userContentController.addUserScript(WKUserScript(source: """
        Object.defineProperty(window, '__LESTER_NATIVE__', {value: Object.freeze({version:1}), writable:false});
        const markLester=()=>{if(document.documentElement){document.documentElement.dataset.lesterNative='ios';return true;}return false;};
        if(!markLester()){const observer=new MutationObserver(()=>{if(markLester())observer.disconnect();});observer.observe(document,{childList:true,subtree:true});}
        """, injectionTime: .atDocumentStart, forMainFrameOnly: true))
        let web = WKWebView(frame: .zero, configuration: configuration)
        web.navigationDelegate = context.coordinator; web.uiDelegate = context.coordinator
        web.isOpaque = false; web.backgroundColor = .systemBackground
        web.scrollView.contentInsetAdjustmentBehavior = .never
        web.allowsBackForwardNavigationGestures = true
        web.isInspectable = _isDebugAssertConfiguration()
        context.coordinator.webView = web
        model.load = { [weak web] url in if server.allowsNavigation(url) { web?.load(URLRequest(url: url)) } }
        model.back = { [weak web] in web?.goBack() }
        model.navigate = { [weak web] command in
            guard let web, let url = web.url, server.acceptsBridge(from: url, mainFrame: true) else { return }
            web.callAsyncJavaScript("window.dispatchEvent(new CustomEvent('lester:native-command',{detail: command}));", arguments: ["command": command], in: nil, contentWorld: .page, completionHandler: nil)
        }
        web.load(URLRequest(url: server.url("/app")))
        return web
    }
    func updateUIView(_ web: WKWebView, context: Context) {}
    static func dismantleUIView(_ web: WKWebView, coordinator: Coordinator) {
        coordinator.cancelDownloads()
        web.stopLoading(); web.configuration.userContentController.removeScriptMessageHandler(forName: "lester")
        web.navigationDelegate = nil; web.uiDelegate = nil
    }
    @MainActor final class Coordinator: NSObject, WKScriptMessageHandler, WKNavigationDelegate, WKUIDelegate, WKDownloadDelegate {
        let model: AppModel
        let server: ServerAddress
        weak var webView: WKWebView?
        private var downloads: [ObjectIdentifier: URL] = [:]
        private var transfers: [ObjectIdentifier: WKDownload] = [:]
        init(model: AppModel, server: ServerAddress) { self.model = model; self.server = server }
        func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
            guard model.server == server, message.webView === webView, message.frameInfo.isMainFrame, let source = message.frameInfo.request.url,
                  server.acceptsBridge(from: source, mainFrame: true), webView?.url.map({ server.acceptsBridge(from: $0, mainFrame: true) }) == true,
                  let body = message.body as? [String: Any], body["version"] as? Int == 1,
                  let type = body["type"] as? String, let payload = body["payload"] as? [String: Any]
            else { return }
            model.receive(type: type, payload: payload)
        }
        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let url = action.request.url else { decisionHandler(.cancel); return }
            if action.targetFrame?.isMainFrame == false { decisionHandler(.allow); return }
            let blobOrigin = url.scheme == "blob" ? URL(string: String(url.absoluteString.dropFirst(5))) : nil
            if action.shouldPerformDownload, action.sourceFrame.isMainFrame,
               action.sourceFrame.request.url.map({ server.acceptsBridge(from: $0, mainFrame: true) }) == true,
               server.sameOrigin(url) || blobOrigin.map(server.sameOrigin) == true { decisionHandler(.download); return }
            if server.allowsNavigation(url) {
                if action.targetFrame == nil { webView.load(action.request); decisionHandler(.cancel) } else { decisionHandler(.allow) }
            } else {
                if action.navigationType == .linkActivated, url.scheme == "https" { UIApplication.shared.open(url) }
                decisionHandler(.cancel)
            }
        }
        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) { model.loading = false; model.canGoBack = webView.canGoBack }
        func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) { fail(error) }
        func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) { fail(error) }
        func webViewWebContentProcessDidTerminate(_ webView: WKWebView) { model.loading = false; model.error = "The workspace needs to reload. Your saved drafts will be restored." }
        private func fail(_ error: Error) {
            guard (error as NSError).code != NSURLErrorCancelled else { return }
            model.loading = false; model.error = "Could not load the workspace. Check your connection and retry."
        }
        func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? { nil }
        func webView(_ webView: WKWebView, navigationAction: WKNavigationAction, didBecome download: WKDownload) { transfers[ObjectIdentifier(download)] = download; download.delegate = self }
        func download(_ download: WKDownload, decideDestinationUsing response: URLResponse, suggestedFilename: String, completionHandler: @escaping (URL?) -> Void) {
            guard model.server == server, response.expectedContentLength >= 0, response.expectedContentLength <= 50 * 1024 * 1024, downloads.count < 4 else { model.error = "Download failed. Please try again."; completionHandler(nil); return }
            let folder = FileManager.default.temporaryDirectory.appendingPathComponent("Lester-\(UUID().uuidString)", isDirectory: true)
            do { try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true); let url = folder.appendingPathComponent(URL(fileURLWithPath: suggestedFilename).lastPathComponent); downloads[ObjectIdentifier(download)] = url; completionHandler(url) }
            catch { completionHandler(nil) }
        }
        func downloadDidFinish(_ download: WKDownload) {
            transfers.removeValue(forKey: ObjectIdentifier(download))
            if let url = downloads.removeValue(forKey: ObjectIdentifier(download)) {
                if model.server == server, model.shareURL == nil { model.shareURL = url }
                else { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
            }
        }
        func download(_ download: WKDownload, didFailWithError error: Error, resumeData: Data?) {
            transfers.removeValue(forKey: ObjectIdentifier(download))
            if let url = downloads.removeValue(forKey: ObjectIdentifier(download)) { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
            if model.server == server { model.error = "Download failed. Please try again." }
        }
        func cancelDownloads() {
            for transfer in transfers.values { transfer.delegate = nil; transfer.cancel { _ in } }
            for url in downloads.values { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
            downloads.removeAll(); transfers.removeAll()
        }
    }
}
