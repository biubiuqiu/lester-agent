package app.lester.client

import androidx.core.net.toUri
import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.graphics.Bitmap
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.util.Base64
import android.webkit.CookieManager
import android.webkit.RenderProcessGoneDetail
import android.webkit.URLUtil
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebStorage
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import app.lester.core.DownloadReceipt
import app.lester.core.ServerAddress
import app.lester.core.parseObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URI
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

@SuppressLint("SetJavaScriptEnabled")
internal class WorkspaceHost(
    private val activity: Activity, private val model: SessionModel, val server: ServerAddress,
    private val chooseFiles: (Intent) -> Unit,
) {
    val web = WebView(activity)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val handler = Handler(Looper.getMainLooper())
    private var fileChooser: ValueCallback<Array<Uri>>? = null
    private var closed = false
    private var broken = false
    private var transfer: Job? = null
    private var chunked: ChunkedDownload? = null
    private data class ChunkedDownload(val receipt: DownloadReceipt, val file: File, val name: String, val mime: String, val created: Long)

    init {
        web.settings.apply {
            javaScriptEnabled = true; domStorageEnabled = true
            allowFileAccess = false; allowContentAccess = false
            mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
            setSupportMultipleWindows(false); javaScriptCanOpenWindowsAutomatically = false
            mediaPlaybackRequiresUserGesture = true
        }
        CookieManager.getInstance().setAcceptCookie(true)
        CookieManager.getInstance().setAcceptThirdPartyCookies(web, false)
        WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG)
        web.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                if (!request.isForMainFrame) return false
                val url = request.url.toString()
                if (server.allowsNavigation(url) || (server.acceptsBridge(web.url.orEmpty()) && server.ownsBlob(url))) return false
                if (request.hasGesture() && request.url.scheme == "https") external(url)
                return true
            }
            override fun onPageStarted(view: WebView, url: String, favicon: Bitmap?) { if (current()) model.loading = true }
            override fun onPageFinished(view: WebView, url: String) { if (current()) { model.loading = false; model.canGoBack = web.canGoBack() } }
            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) { if (request.isForMainFrame && current()) { model.loading = false; model.error = R.string.load_failed } }
            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                broken = true; model.loading = false; model.error = R.string.workspace_restart
                (web.parent as? android.view.ViewGroup)?.removeView(web); web.destroy()
                return true
            }
        }
        web.webChromeClient = object : WebChromeClient() {
            override fun onShowFileChooser(view: WebView, callback: ValueCallback<Array<Uri>>, params: FileChooserParams): Boolean {
                fileChooser?.onReceiveValue(null); fileChooser = callback
                if (!server.acceptsBridge(web.url.orEmpty())) { receiveFiles(null); return true }
                return try { chooseFiles(params.createIntent().apply { addCategory(Intent.CATEGORY_OPENABLE) }); true }
                catch (_: Exception) { receiveFiles(null); model.error = R.string.file_picker_failed; true }
            }
        }
        web.setDownloadListener { url, _, disposition, mime, length -> download(url, disposition, mime ?: "application/octet-stream", length) }
        model.command = { action, id ->
            if (current() && server.acceptsBridge(web.url.orEmpty())) {
                val data = JSONObject().put("action", action).apply { if (id != null) put("id", id) }
                web.evaluateJavascript("window.dispatchEvent(new CustomEvent('lester:native-command',{detail:JSON.parse(${JSONObject.quote(data.toString())})}));", null)
            }
        }
        model.reload = { if (broken) model.webRevision++ else { model.error = null; web.loadUrl(server.url("/app")) } }
        if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            WebViewCompat.addWebMessageListener(web, "LesterAndroid", setOf(server.origin)) { view, message, source, mainFrame, _ ->
                if (view === web && current() && mainFrame && server.sameOrigin(source.toString()) && server.acceptsBridge(web.url.orEmpty())) {
                    runCatching {
                        val envelope = parseObject(message.data.orEmpty())
                        require(envelope.getInt("version") == 1)
                        val type = envelope.getString("type"); val payload = envelope.getJSONObject("payload")
                        if (type.startsWith("download_")) receiveDownload(type, payload) else { model.receive(type, payload); model.canGoBack = web.canGoBack() }
                    }.onFailure { if (chunked != null) { cancelDownload(); model.error = R.string.download_failed } }
                }
            }
            if (WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)) {
                WebViewCompat.addDocumentStartJavaScript(web, """
                    if(window.top===window){
                      Object.defineProperty(window,'__LESTER_NATIVE__',{value:Object.freeze({version:1,platform:'android'}),writable:false});
                      const mark=()=>{if(document.documentElement){document.documentElement.dataset.lesterNative='android';return true;}return false;};
                      if(!mark()){const observer=new MutationObserver(()=>{if(mark())observer.disconnect();});observer.observe(document,{childList:true,subtree:true});}
                    }
                """.trimIndent(), setOf(server.origin))
                web.loadUrl(server.url("/app"))
            } else unsupported()
        } else unsupported()
    }
    private fun unsupported() { model.loading = false; model.error = R.string.update_webview; model.reload = { model.loading = false; model.error = R.string.update_webview } }
    private fun current() = !closed && model.server == server
    fun receiveFiles(uris: Array<Uri>?) { fileChooser?.onReceiveValue(uris); fileChooser = null }
    fun active(value: Boolean) { if (value) web.onResume() else { web.onPause(); cancelDownload() } }
    fun back(exit: () -> Unit) {
        if (broken) { exit(); return }
        // Respect an open Web dialog before navigating away from its draft.
        web.evaluateJavascript("(()=>{const d=document.querySelector('dialog[open]');if(!d)return false;d.dispatchEvent(new Event('cancel',{cancelable:true}));return true;})()") { handled ->
            if (handled != "true") { if (web.canGoBack()) web.goBack() else exit() }
        }
    }
    private fun external(url: String) { runCatching { activity.startActivity(Intent(Intent.ACTION_VIEW, url.toUri()).addCategory(Intent.CATEGORY_BROWSABLE)) }.onFailure { model.error = R.string.browser_failed } }
    private fun download(url: String, disposition: String?, mime: String, length: Long) {
        if (!current() || !model.foregroundActive || !server.acceptsBridge(web.url.orEmpty()) || model.export != null || length > MAX_DOWNLOAD) return
        if (!server.ownsBlob(url) && !server.sameOrigin(url)) return
        cancelDownload()
        val name = URLUtil.guessFileName(url, disposition, mime).replace('\\', '/').substringAfterLast('/').take(180).takeUnless { it in listOf("", ".", "..") } ?: "download"
        val file = File.createTempFile("lester-export-", ".tmp", activity.cacheDir)
        if (server.ownsBlob(url)) {
            val token = random()
            chunked = ChunkedDownload(DownloadReceipt(token), file, name, mime, System.currentTimeMillis())
            handler.postDelayed({ if (chunked?.receipt?.id == token) { cancelDownload(); model.error = R.string.download_failed } }, 60_000)
            web.evaluateJavascript("""
              (async()=>{const token=${JSONObject.quote(token)},send=(type,payload)=>window.LesterAndroid.postMessage(JSON.stringify({version:1,type,payload}));
                window.__lesterDownloadToken=token;
                try{const blob=await(await fetch(${JSONObject.quote(url)})).blob();if(blob.size>$MAX_DOWNLOAD)throw Error();
                  send('download_meta',{id:token,size:blob.size});
                  for(let offset=0,part=0;offset<blob.size||part===0;offset+=65536,part++){
                    if(window.__lesterDownloadToken!==token)return;
                    const data=await new Promise((ok,fail)=>{const reader=new FileReader();reader.onload=()=>ok(reader.result.split(',')[1]);reader.onerror=fail;reader.readAsDataURL(blob.slice(offset,offset+65536));});
                    send('download_chunk',{id:token,part,bytes:data,last:offset+65536>=blob.size});
                  }
                }catch{send('download_error',{id:token});}
              })();
            """.trimIndent(), null)
        } else {
            val cookie = CookieManager.getInstance().getCookie(url)
            transfer = scope.launch {
                try {
                    withContext(Dispatchers.IO) {
                        val connection = URI(url).toURL().openConnection() as HttpURLConnection
                        try {
                            connection.instanceFollowRedirects = false; connection.connectTimeout = 15_000; connection.readTimeout = 20_000; connection.useCaches = false
                            if (cookie != null) connection.setRequestProperty("Cookie", cookie)
                            check(connection.responseCode == 200 && connection.contentLengthLong <= MAX_DOWNLOAD)
                            connection.inputStream.use { input -> file.outputStream().use { output ->
                                val buffer = ByteArray(64 * 1024); var total = 0L
                                while (true) { ensureActive(); val count = input.read(buffer); if (count < 0) break; total += count; check(total <= MAX_DOWNLOAD); output.write(buffer, 0, count) }
                            } }
                        } finally { connection.disconnect() }
                    }
                    if (current() && model.foregroundActive) model.export = ExportFile(file, name, mime) else file.delete()
                } catch (_: Exception) { file.delete(); if (current() && model.foregroundActive) model.error = R.string.download_failed }
            }
        }
    }
    private fun receiveDownload(type: String, payload: JSONObject) {
        val pending = chunked ?: return
        require(payload.getString("id") == pending.receipt.id && System.currentTimeMillis() - pending.created < 60_000)
        when (type) {
            "download_meta" -> pending.receipt.begin(payload.getString("id"), payload.getLong("size"))
            "download_chunk" -> {
                val text = payload.getString("bytes"); require(text.length <= 88_000)
                val bytes = pending.receipt.accept(payload.getString("id"), payload.getInt("part"), Base64.decode(text, Base64.DEFAULT), payload.getBoolean("last"))
                pending.file.appendBytes(bytes)
                if (pending.receipt.complete) { chunked = null; model.export = ExportFile(pending.file, pending.name, pending.mime) }
            }
            else -> { cancelDownload(); model.error = R.string.download_failed }
        }
    }
    private fun cancelDownload() {
        transfer?.cancel(); transfer = null
        chunked?.file?.delete(); chunked = null
        if (!broken && !closed) web.evaluateJavascript("window.__lesterDownloadToken=null;", null)
    }
    fun close() {
        cancelDownload(); closed = true; scope.cancel(); receiveFiles(null)
        if (!broken) { web.stopLoading(); web.webChromeClient = null; web.destroy() }
    }
    companion object {
        const val MAX_DOWNLOAD = 50L * 1024 * 1024
        fun clearData(done: () -> Unit) {
            WebStorage.getInstance().deleteAllData()
            CookieManager.getInstance().removeAllCookies { CookieManager.getInstance().flush(); done() }
        }
    }
}
