package app.lester.client

import android.app.Application
import android.content.Context
import android.os.Build
import android.os.VibrationEffect
import android.os.Vibrator
import android.os.VibratorManager
import android.webkit.CookieManager
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.core.content.edit
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import app.lester.core.CompletionGate
import app.lester.core.ServerAddress
import app.lester.core.WorkspaceSnapshot
import java.io.File
import java.security.MessageDigest
import kotlinx.coroutines.launch
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.coroutines.Job
import kotlinx.coroutines.suspendCancellableCoroutine
import org.json.JSONArray
import org.json.JSONObject
import kotlin.coroutines.resume

internal data class ExportFile(val file: File, val name: String, val mime: String)

internal class SessionModel(application: Application) : AndroidViewModel(application) {
    private val prefs = application.getSharedPreferences("lester", Context.MODE_PRIVATE)
    private val signInStore = SignInStore(application)
    var server by mutableStateOf(prefs.getString("server", null)?.let { runCatching { ServerAddress.parse(it, BuildConfig.DEBUG) }.getOrNull() }); private set
    var workspace by mutableStateOf<WorkspaceSnapshot?>(null); private set
    var needsSignIn by mutableStateOf(false); private set
    var signingIn by mutableStateOf(false); private set
    var loading by mutableStateOf(true)
    var error by mutableStateOf<Int?>(null)
    var webRevision by mutableIntStateOf(0)
    var canGoBack by mutableStateOf(false)
    var clearingData by mutableStateOf(false); private set
    var export by mutableStateOf<ExportFile?>(null)
    var haptics by mutableStateOf(prefs.getBoolean("haptics", true)); private set
    private var foreground = false
    val foregroundActive get() = foreground
    private var gate = CompletionGate()
    private var authJob: Job? = null
    private var authGeneration = 0L
    var command: ((String, String?) -> Unit)? = null
    var reload: (() -> Unit)? = null
    init { restoreReceipts() }
    fun connect(input: String) {
        if (clearingData) return
        runCatching { ServerAddress.parse(input, BuildConfig.DEBUG) }.onSuccess {
            server = it; workspace = null; needsSignIn = false; error = null; loading = true
            prefs.edit { putString("server", it.origin) }; restoreReceipts()
        }.onFailure { error = R.string.invalid_server }
    }
    fun haptics(value: Boolean) { haptics = value; prefs.edit { putBoolean("haptics", value) } }
    fun receive(type: String, payload: JSONObject) {
        when (type) {
            "workspace" -> {
                val snapshot = WorkspaceSnapshot.parse(payload)
                workspace = snapshot; gate.observe(snapshot); needsSignIn = false; loading = false; error = null
            }
            "signed_out" -> { workspace = null; gate.reset(); needsSignIn = true; loading = false }
            "route" -> loading = false
            "run_completed" -> {
                if (workspace?.account != payload.optString("account")) return
                val vibrate = gate.complete(payload, foreground)
                prefs.edit { putString(receiptKey(), JSONArray(gate.delivered).toString()) }
                if (vibrate && haptics) vibrateCompletion()
            }
        }
    }
    fun active(value: Boolean) { foreground = value; if (!value) gate.reset() else command?.invoke("foreground", null) }
    private fun vibrateCompletion() {
        val context = getApplication<Application>()
        val vibrator = if (Build.VERSION.SDK_INT >= 31) context.getSystemService(VibratorManager::class.java).defaultVibrator else @Suppress("DEPRECATION") (context.getSystemService(Context.VIBRATOR_SERVICE) as Vibrator)
        if (vibrator.hasVibrator()) vibrator.vibrate(VibrationEffect.createOneShot(45, VibrationEffect.DEFAULT_AMPLITUDE))
    }
    fun signIn(openBrowser: (String) -> Unit) {
        val configured = server ?: return
        if (signingIn) return
        signingIn = true
        error = null
        val generation = ++authGeneration
        authJob = viewModelScope.launch {
            try {
                val pending = NativeSignIn.pending(configured)
                withContext(Dispatchers.IO) { signInStore.save(pending) }
                if (server == configured && generation == authGeneration) openBrowser(configured.loginUrl(NativeSignIn.challenge(pending.verifier), pending.state))
                else withContext(Dispatchers.IO) { signInStore.clear() }
            } catch (_: Exception) { if (server == configured && generation == authGeneration) error = R.string.sign_in_failed }
            finally { if (generation == authGeneration) signingIn = false }
        }
    }
    fun callback(uri: String) {
        if (signingIn) return
        signingIn = true
        error = null
        val generation = ++authGeneration
        authJob = viewModelScope.launch {
            val configured = server
            try {
                val pending = withContext(Dispatchers.IO) { signInStore.read() }
                val code = pending?.let { ServerAddress.callbackCode(uri, it.state) }
                check(configured != null && server == configured && pending != null && pending.server == configured.origin && code != null)
                // Consume local state before the one-shot POST. Unknown results require a fresh sign-in.
                withContext(Dispatchers.IO) { signInStore.clear() }
                val headers = NativeSignIn.exchange(configured, code, pending.verifier)
                if (server != configured || generation != authGeneration) return@launch
                for (header in headers) {
                    check(server == configured && generation == authGeneration)
                    check(suspendCancellableCoroutine<Boolean> { done -> CookieManager.getInstance().setCookie(configured.origin, header) { if (done.isActive) done.resume(it) } })
                }
                check(server == configured && generation == authGeneration)
                CookieManager.getInstance().flush()
                needsSignIn = false; loading = true; reload?.invoke()
            } catch (_: Exception) { if (server == configured && generation == authGeneration) error = R.string.sign_in_failed }
            finally { if (generation == authGeneration) signingIn = false }
        }
    }
    fun disconnect(clearWebData: (() -> Unit) -> Unit) {
        if (clearingData) return
        authGeneration++
        authJob?.cancel()
        authJob = null
        signingIn = false
        clearingData = true; server = null; workspace = null; gate.reset(); signInStore.clear(); dismissExport()
        prefs.edit { remove("server") }
        clearWebData { clearingData = false; needsSignIn = false; error = null; loading = true }
    }
    fun dismissExport() { export?.file?.delete(); export = null }
    private fun receiptKey(): String = "completion." + MessageDigest.getInstance("SHA-256").digest((server?.origin ?: "").toByteArray()).joinToString("") { "%02x".format(it) }
    private fun restoreReceipts() {
        val delivered = runCatching { val items = JSONArray(prefs.getString(receiptKey(), "[]")); (0 until items.length()).map { items.getString(it) } }.getOrDefault(emptyList())
        gate = CompletionGate(delivered)
    }
}
