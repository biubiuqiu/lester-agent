package app.lester.client

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import app.lester.core.ServerAddress
import java.net.HttpCookie
import java.net.HttpURLConnection
import java.net.URI
import java.security.KeyStore
import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject

internal data class PendingSignIn(val server: String, val verifier: String, val state: String, val expires: Long)

/** A browser handoff can outlive an Activity/process. Never persist its verifier in plaintext. */
internal class SignInStore(context: Context) {
    private val prefs = context.getSharedPreferences("native-sign-in", Context.MODE_PRIVATE)
    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey("lester.pkce", null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").run {
            init(KeyGenParameterSpec.Builder("lester.pkce", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
            generateKey()
        }
    }
    fun save(value: PendingSignIn) {
        val plain = JSONObject().put("server", value.server).put("verifier", value.verifier).put("state", value.state).put("expires", value.expires).toString()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key()) }
        val bytes = cipher.doFinal(plain.toByteArray(Charsets.UTF_8))
        // Called on Dispatchers.IO; require durable state before opening the browser.
        check(prefs.edit().putString("pending", encode(cipher.iv) + "." + encode(bytes)).commit())
    }
    fun read(): PendingSignIn? = runCatching {
        val pieces = (prefs.getString("pending", null) ?: return null).split('.')
        require(pieces.size == 2)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, decode(pieces[0]))) }
        val json = JSONObject(String(cipher.doFinal(decode(pieces[1])), Charsets.UTF_8))
        PendingSignIn(json.getString("server"), json.getString("verifier"), json.getString("state"), json.getLong("expires"))
            .also { require(it.expires > System.currentTimeMillis()) }
    }.getOrNull()
    fun clear() { check(prefs.edit().remove("pending").commit()) }
}

internal object NativeSignIn {
    fun pending(server: ServerAddress): PendingSignIn = PendingSignIn(server.origin, random(), random(), System.currentTimeMillis() + 10 * 60_000)
    fun challenge(verifier: String): String = encode(MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray(Charsets.US_ASCII)))
    suspend fun exchange(server: ServerAddress, code: String, verifier: String): List<String> = withContext(Dispatchers.IO) {
        val connection = URI(server.url("/api/v1/auth/native/exchange")).toURL().openConnection() as HttpURLConnection
        try {
            connection.instanceFollowRedirects = false; connection.connectTimeout = 15_000; connection.readTimeout = 20_000
            connection.requestMethod = "POST"; connection.doOutput = true; connection.useCaches = false
            connection.setRequestProperty("Content-Type", "application/json"); connection.setRequestProperty("Origin", server.origin)
            val bytes = JSONObject().put("code", code).put("verifier", verifier).toString().toByteArray(Charsets.UTF_8)
            connection.setFixedLengthStreamingMode(bytes.size)
            connection.outputStream.use { it.write(bytes) }
            check(connection.responseCode == 204)
            val headers = connection.headerFields.entries.filter { it.key.equals("Set-Cookie", true) }.flatMap { it.value }
            val paths = mapOf("lester_access_token" to "/", "lester_refresh_token" to "/api/v1/auth")
            check(headers.size == 2)
            val names = headers.map { header ->
                check(header.length <= 8192)
                val cookie = HttpCookie.parse(header).single()
                check(cookie.name in paths && cookie.path == paths[cookie.name] && cookie.isHttpOnly && (server.development || cookie.secure))
                check(cookie.domain == null || cookie.domain.equals(URI(server.origin).host, true))
                check(ServerAddress.credential.matches(cookie.value) && cookie.maxAge != 0L)
                cookie.name
            }
            check(names.toSet() == paths.keys)
            headers
        } finally { connection.disconnect() }
    }
}
internal fun random(): String = encode(ByteArray(32).also { SecureRandom().nextBytes(it) })
private fun encode(bytes: ByteArray): String = Base64.encodeToString(bytes, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
private fun decode(text: String): ByteArray = Base64.decode(text, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
