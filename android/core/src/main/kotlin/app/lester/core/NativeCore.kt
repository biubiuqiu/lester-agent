package app.lester.core

import java.net.URI
import java.net.URLDecoder
import java.net.URLEncoder
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.UUID
import org.json.JSONObject
import org.json.JSONTokener

private val uuidPattern = Regex("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")
fun validId(value: String): Boolean = uuidPattern.matches(value)
private fun checkedId(value: String): String { require(validId(value)); return UUID.fromString(value).toString() }
fun parseObject(text: String): JSONObject {
    require(text.toByteArray().size <= 512_000)
    val parser = JSONTokener(text)
    val value = parser.nextValue()
    require(value is JSONObject)
    require(parser.nextClean() == '\u0000')
    return value
}

@ConsistentCopyVisibility
data class ServerAddress private constructor(val origin: String, val development: Boolean) {
    private val address get() = URI(origin)
    fun url(path: String): String { require(path.startsWith("/") && !path.startsWith("//")); return origin + path }
    fun sameOrigin(value: String): Boolean = runCatching {
        val input = URI(value)
        fun port(uri: URI) = if (uri.port >= 0) uri.port else if (uri.scheme.equals("https", true)) 443 else 80
        input.rawUserInfo == null && input.scheme.equals(address.scheme, true) && input.host.equals(address.host, true) && port(input) == port(address)
    }.getOrDefault(false)
    fun acceptsBridge(value: String, mainFrame: Boolean = true): Boolean = mainFrame && sameOrigin(value) && runCatching {
        val path = URI(value).path
        path == "/app" || path.startsWith("/app/") || path == "/login"
    }.getOrDefault(false)
    fun allowsNavigation(value: String): Boolean = sameOrigin(value) && runCatching {
        val path = URI(value).path
        path == "/" || path == "/login" || path == "/app" || path.startsWith("/app/") || path == "/docs" || path.startsWith("/docs/") || path.startsWith("/preview/")
    }.getOrDefault(false)
    fun ownsBlob(value: String): Boolean = value.startsWith("blob:") && sameOrigin(value.removePrefix("blob:"))
    fun loginUrl(challenge: String, state: String): String {
        require(credential.matches(challenge) && credential.matches(state))
        val target = "/app/native-auth?challenge=$challenge&state=$state"
        return url("/login?returnTo=" + URLEncoder.encode(target, "UTF-8"))
    }
    companion object {
        val credential = Regex("^[A-Za-z0-9_-]{43}$")
        fun parse(text: String, debug: Boolean = false): ServerAddress {
            val uri = URI(text.trim())
            val host = requireNotNull(uri.host).lowercase()
            val scheme = uri.scheme?.lowercase()
            require(uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null)
            require(uri.rawPath in listOf(null, "", "/", "/app", "/app/"))
            require(uri.port == -1 || uri.port in 1..65535)
            val local = debug && scheme == "http" && host in listOf("localhost", "127.0.0.1", "10.0.2.2", "[::1]")
            require(scheme == "https" || local)
            return ServerAddress(URI(scheme, null, host, uri.port, null, null, null).toASCIIString(), local)
        }
        fun callbackCode(text: String, state: String): String? = runCatching {
            val uri = URI(text)
            require(uri.scheme == "lester" && uri.host == "auth" && uri.path == "/callback" && uri.port == -1 && uri.rawUserInfo == null && uri.rawFragment == null)
            val fields = requireNotNull(uri.rawQuery).split('&').map {
                val pair = it.split('=', limit = 2); require(pair.size == 2)
                URLDecoder.decode(pair[0], "UTF-8") to URLDecoder.decode(pair[1], "UTF-8")
            }
            require(fields.size == 2 && fields.map { it.first }.toSet() == setOf("code", "state"))
            require(MessageDigest.isEqual(fields.first { it.first == "state" }.second.toByteArray(), state.toByteArray()))
            fields.first { it.first == "code" }.second.also { require(credential.matches(it)) }
        }.getOrNull()
    }
}

data class NativeConversation(val id: String, val title: String, val projectName: String, val runId: String?, val runStatus: String)
data class WorkspaceSnapshot(val account: String, val displayName: String, val currentId: String?, val locale: String, val conversations: List<NativeConversation>) {
    companion object {
        fun parse(json: JSONObject): WorkspaceSnapshot {
            val user = checkedId(json.getString("user_id")); val workspace = checkedId(json.getString("workspace_id"))
            val locale = json.getString("locale"); require(locale in listOf("zh-CN", "en", "ja", "ko", "fr", "es"))
            val displayName = json.getString("display_name").bounded(512)
            val projects = json.getJSONArray("projects"); require(projects.length() <= 100)
            val projectIds = (0 until projects.length()).map { index ->
                val item = projects.getJSONObject(index); item.getString("name").bounded(512); checkedId(item.getString("id"))
            }
            require(projectIds.toSet().size == projectIds.size)
            val items = json.getJSONArray("conversations"); require(items.length() <= 200)
            val conversations = (0 until items.length()).map { index ->
                val item = items.getJSONObject(index)
                checkedId(item.getString("project_id"))
                NativeConversation(checkedId(item.getString("id")), item.getString("title").bounded(2048), item.getString("project_name").bounded(512),
                    if (item.isNull("run_id")) null else checkedId(item.getString("run_id")), item.getString("run_status").bounded(32))
            }
            require(conversations.map { it.id }.toSet().size == conversations.size)
            return WorkspaceSnapshot("$user:$workspace", displayName, if (json.isNull("current_id")) null else checkedId(json.getString("current_id")), locale, conversations)
        }
        private fun String.bounded(max: Int): String { require(toByteArray(StandardCharsets.UTF_8).size <= max); return this }
    }
}

class CompletionGate(delivered: List<String> = emptyList()) {
    private var account = ""
    private val active = mutableMapOf<String, String>()
    private val receipts = LinkedHashSet(delivered.takeLast(256))
    val delivered get() = receipts.toList()
    fun observe(snapshot: WorkspaceSnapshot) {
        account = snapshot.account; active.clear()
        for (item in snapshot.conversations) {
            if (item.runId != null && item.runStatus in listOf("running", "cancelling") && receipt(item.runId) !in receipts) active[item.runId] = item.id
        }
    }
    private fun receipt(run: String) = "$account:$run"
    fun complete(payload: JSONObject, foreground: Boolean): Boolean {
        val run = payload.getString("run_id")
        if (payload.getString("account") != account || payload.getLong("event_id") <= 0 || active[run] != payload.getString("conversation_id")) return false
        active.remove(run)
        if (!receipts.add(receipt(run))) return false
        while (receipts.size > 256) receipts.remove(receipts.first())
        return foreground
    }
    fun reset() { account = ""; active.clear() }
}

/** Only a pending user-initiated download can receive ordered bounded chunks. */
class DownloadReceipt(val id: String, val limit: Long = 50L * 1024 * 1024) {
    private var size = -1L
    private var received = 0L
    private var part = 0
    private var ended = false
    fun begin(token: String, bytes: Long) { require(token == id && size == -1L && bytes in 0..limit); size = bytes }
    fun accept(token: String, index: Int, bytes: ByteArray, last: Boolean): ByteArray {
        require(token == id && !ended && size >= 0 && index == part && part < 1024 && bytes.size <= 64 * 1024)
        require(received + bytes.size <= size)
        received += bytes.size; part++
        if (last) { require(received == size); ended = true }
        return bytes
    }
    val complete get() = ended
}
