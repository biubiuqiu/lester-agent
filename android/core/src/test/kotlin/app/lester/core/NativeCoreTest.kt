package app.lester.core

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class NativeCoreTest {
    private fun rejects(block: () -> Unit) { try { block(); fail("Unexpectedly accepted") } catch (_: IllegalArgumentException) {} }
    @Test fun originsAreStrictAndDebugHttpIsLocalOnly() {
        val server = ServerAddress.parse(" https://lester.example:443/app ")
        assertTrue(server.sameOrigin("https://lester.example/app"))
        for (url in listOf("https://lester.example.evil.test/app", "https://lester.example:444/app", "https://user@lester.example/app")) assertFalse(server.sameOrigin(url))
        for (url in listOf("http://lester.example", "https://user:pass@lester.example", "https://lester.example/other", "https://lester.example?q=1", "https://lester.example/#token")) rejects { ServerAddress.parse(url) }
        rejects { ServerAddress.parse("http://10.0.2.2:13000") }
        assertTrue(ServerAddress.parse("http://10.0.2.2:13000", debug = true).development)
        rejects { ServerAddress.parse("http://remote.example", debug = true) }
    }
    @Test fun onlyTheConfiguredMainAppFrameHasBridgeAuthority() {
        val server = ServerAddress.parse("https://lester.example")
        assertTrue(server.acceptsBridge(server.url("/app/c/123")))
        assertFalse(server.acceptsBridge(server.url("/app"), mainFrame = false))
        for (path in listOf("/preview/123", "/docs", "/api/v1/me", "/app-evil")) assertFalse(server.acceptsBridge(server.url(path)))
        assertTrue(server.allowsNavigation(server.url("/preview/123")))
        assertTrue(server.ownsBlob("blob:https://lester.example/123"))
        assertFalse(server.ownsBlob("blob:https://evil.example/123"))
    }
    @Test fun browserCallbackRequiresOneCodeAndMatchingState() {
        val code = "c".repeat(43); val state = "s".repeat(43)
        assertEquals(code, ServerAddress.callbackCode("lester://auth/callback?code=$code&state=$state", state))
        for (url in listOf("lester://auth:443/callback?code=$code&state=$state", "lester://auth/callback?code=$code&state=wrong", "lester://auth/callback?code=$code&state=$state&state=$state", "lester://auth/callback?code=$code&state=$state#secret", "https://auth/callback?code=$code&state=$state")) assertNull(ServerAddress.callbackCode(url, state))
        assertTrue(ServerAddress.parse("https://lester.example").loginUrl(code, state).contains("returnTo=%2Fapp%2Fnative-auth"))
    }
    private val run = "33333333-3333-4333-8333-333333333333"
    private val conversation = "44444444-4444-4444-8444-444444444444"
    private val account = "11111111-1111-4111-8111-111111111111:22222222-2222-4222-8222-222222222222"
    private fun snapshot(status: String = "running") = WorkspaceSnapshot(account, "User", conversation, "en", listOf(NativeConversation(conversation, "Task", "Product", run, status)))
    private fun completion() = JSONObject().put("account", account).put("run_id", run).put("conversation_id", conversation).put("event_id", 12)
    @Test fun hapticsAreForegroundOnceAndReceiptsSurviveReopening() {
        val gate = CompletionGate(); assertFalse(gate.complete(completion(), true))
        gate.observe(snapshot()); assertFalse(gate.complete(completion().put("account", "other"), true))
        assertTrue(gate.complete(completion(), true)); gate.observe(snapshot()); assertFalse(gate.complete(completion(), true))
        val reopened = CompletionGate(gate.delivered); reopened.observe(snapshot()); assertFalse(reopened.complete(completion(), true))
    }
    @Test fun backgroundAndHistoricalRunsNeverProduceCatchupHaptics() {
        val gate = CompletionGate(); gate.observe(snapshot("completed")); assertFalse(gate.complete(completion(), true))
        gate.observe(snapshot()); assertFalse(gate.complete(completion(), false)); gate.observe(snapshot()); assertFalse(gate.complete(completion(), true))
        gate.reset(); assertFalse(gate.complete(completion(), true))
    }
    @Test fun downloadChunksCannotEscapeTheirUserInitiatedBoundedReceipt() {
        val receipt = DownloadReceipt("secret", 10)
        rejects { receipt.begin("other", 3) }; rejects { receipt.begin("secret", 11) }; receipt.begin("secret", 3)
        rejects { receipt.accept("secret", 1, byteArrayOf(1), false) }
        rejects { receipt.accept("other", 0, byteArrayOf(1), false) }
        receipt.accept("secret", 0, byteArrayOf(1), false)
        rejects { receipt.accept("secret", 1, byteArrayOf(2,3,4), true) }
        receipt.accept("secret", 1, byteArrayOf(2,3), true); assertTrue(receipt.complete)
        rejects { receipt.accept("secret", 2, byteArrayOf(), true) }
    }
    @Test fun bridgeEnvelopesAreBoundedObjectsWithNoTrailingPayload() {
        assertEquals(1, parseObject("{\"version\":1}").getInt("version"))
        rejects { parseObject("{} {}") }; rejects { parseObject("[]") }; rejects { parseObject(" ".repeat(512001)) }
    }
}
