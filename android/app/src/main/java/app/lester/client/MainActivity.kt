package app.lester.client

import android.content.Intent
import android.os.Bundle
import android.webkit.WebChromeClient
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.lifecycle.lifecycleScope
import androidx.browser.customtabs.CustomTabsIntent
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.platform.LocalDensity
import androidx.core.net.toUri
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.foundation.text.KeyboardOptions
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Locale

class MainActivity : ComponentActivity() {
    private val model: SessionModel by viewModels()
    private var host: WorkspaceHost? = null
    private val picker = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        host?.receiveFiles(WebChromeClient.FileChooserParams.parseResult(result.resultCode, result.data))
    }
    private val save = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        val file = model.export
        val uri = result.data?.data
        if (result.resultCode != RESULT_OK || uri == null || file == null) { model.dismissExport(); return@registerForActivityResult }
        lifecycleScope.launch {
            try { withContext(Dispatchers.IO) { contentResolver.openOutputStream(uri, "w")!!.use { out -> file.file.inputStream().use { it.copyTo(out) } } } }
            catch (_: Exception) { model.error = R.string.download_failed }
            finally { model.dismissExport() }
        }
    }
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState); enableEdgeToEdge()
        // Cache exports are private, temporary, and never included in backup.
        cacheDir.listFiles()?.filter { it.name.startsWith("lester-export-") && System.currentTimeMillis() - it.lastModified() > 24 * 60 * 60_000 }?.forEach { it.delete() }
        setContent {
            val base = LocalContext.current
            val configuration = LocalConfiguration.current
            val locale = model.workspace?.locale
            val localized = remember(base, configuration, locale) {
                android.content.res.Configuration(configuration).apply { if (locale != null) setLocale(Locale.forLanguageTag(locale)) }
            }
            val context = remember(base, localized) { base.createConfigurationContext(localized) }
            CompositionLocalProvider(LocalContext provides context, LocalConfiguration provides localized) { LesterTheme { LesterApp(model) } }
        }
        handleCallback(intent)
    }
    override fun onNewIntent(intent: Intent) { super.onNewIntent(intent); setIntent(intent); handleCallback(intent) }
    override fun onConfigurationChanged(newConfig: android.content.res.Configuration) {
        super.onConfigurationChanged(newConfig)
        enableEdgeToEdge()
    }
    private fun handleCallback(intent: Intent) {
        val callback = intent.data?.toString() ?: return
        intent.data = null // Do not replay a consumed callback on Activity/process recreation.
        model.callback(callback)
    }
    override fun onStart() { super.onStart(); host?.active(true); model.active(true) }
    override fun onStop() { model.active(false); host?.active(false); super.onStop() }
    override fun onDestroy() { host?.close(); host = null; super.onDestroy() }
    private fun signIn() {
        model.signIn { url ->
            try { CustomTabsIntent.Builder().setShowTitle(false).setShareState(CustomTabsIntent.SHARE_STATE_OFF).build().launchUrl(this, url.toUri()) }
            catch (_: Exception) { model.error = R.string.browser_failed }
        }
    }

    @OptIn(ExperimentalMaterial3Api::class)
    @Composable private fun LesterApp(model: SessionModel) {
        val configured = model.server
        var settings by remember { mutableStateOf(false) }
        var confirmServer by remember { mutableStateOf(false) }
        val drawer = rememberDrawerState(DrawerValue.Closed)
        val scope = rememberCoroutineScope()
        val wide = with(LocalDensity.current) { LocalWindowInfo.current.containerSize.width.toDp() } >= 840.dp
        BackHandler(enabled = configured != null) { if (drawer.isOpen) scope.launch { drawer.close() } else host?.back { moveTaskToBack(true) } }
        Surface(Modifier.fillMaxSize()) {
            if (configured == null) Connect(model)
            else {
                // Moving between drawer and tablet columns retains the same WebView,
                // including selected local File objects and unsent attachments.
                val webContent = remember(configured, model.webRevision) { movableContentOf {
                    AndroidView(factory = {
                        host?.close()
                        WorkspaceHost(this@MainActivity, model, configured) { picker.launch(it) }.also { host = it }.web
                    }, modifier = Modifier.fillMaxSize(), onReset = null, onRelease = { view ->
                        if (host?.web === view) { host?.close(); host = null }
                    })
                } }
                val content: @Composable () -> Unit = {
                    Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing).imePadding()) {
                        TopAppBar(title = {}, navigationIcon = {
                            if (!wide && model.workspace != null) NativeIcon(R.drawable.ic_menu, R.string.recent) { scope.launch { drawer.open() } }
                            else if (model.canGoBack) NativeIcon(R.drawable.ic_back, R.string.back) { host?.back { moveTaskToBack(true) } }
                        }, actions = {
                            if (model.workspace != null) NativeIcon(R.drawable.ic_lester, R.string.personal_assistant) { model.command?.invoke("home", null) }
                            NativeIcon(R.drawable.ic_settings, R.string.settings) { settings = true }
                        }, colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surface))
                        model.error?.let { message ->
                            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                                Text(stringResource(message), Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                                TextButton(onClick = { if (model.needsSignIn) signIn() else model.reload?.invoke() }) { Text(stringResource(R.string.retry)) }
                            }
                        }
                        Box(Modifier.fillMaxWidth().weight(1f)) {
                            webContent()
                            if (model.needsSignIn) Surface(Modifier.fillMaxSize()) {
                                Column(Modifier.padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                                    Text("Lester", style = MaterialTheme.typography.headlineLarge)
                                    Text(stringResource(R.string.sign_in_description), Modifier.padding(vertical = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    Button(onClick = { signIn() }, enabled = !model.signingIn) { Text(stringResource(R.string.sign_in)) }
                                }
                            }
                            if (model.loading && !model.needsSignIn) CircularProgressIndicator(Modifier.align(Alignment.Center))
                        }
                    }
                }
                if (wide && model.workspace != null) Row(Modifier.fillMaxSize()) {
                    Surface(Modifier.width(280.dp).fillMaxHeight(), color = MaterialTheme.colorScheme.surfaceContainerLow) { Recent(model) { model.command?.invoke("conversation", it) } }
                    Box(Modifier.weight(1f)) { content() }
                } else ModalNavigationDrawer(drawerState = drawer, gesturesEnabled = model.workspace != null, drawerContent = {
                    ModalDrawerSheet { Recent(model) { model.command?.invoke("conversation", it); scope.launch { drawer.close() } } }
                }, content = content)
            }
        }
        if (settings && configured != null) ModalBottomSheet(onDismissRequest = { settings = false }) {
            Column(Modifier.fillMaxWidth().padding(horizontal = 24.dp).padding(bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(stringResource(R.string.settings), style = MaterialTheme.typography.titleLarge)
                Text(configured.origin, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                model.workspace?.let {
                    Text(it.displayName)
                    TextButton(onClick = { settings = false; model.command?.invoke("settings", null) }) { Text(stringResource(R.string.account)) }
                    TextButton(onClick = { settings = false; model.command?.invoke("tasks", null) }) { Text(stringResource(R.string.schedules)) }
                }
                Row(verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.haptics), Modifier.weight(1f)); Switch(checked = model.haptics, onCheckedChange = { model.haptics(it) }) }
                Text(stringResource(R.string.haptics_description), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                TextButton(onClick = { confirmServer = true }) { Text(stringResource(R.string.change_server), color = MaterialTheme.colorScheme.error) }
            }
        }
        if (confirmServer) AlertDialog(onDismissRequest = { confirmServer = false }, title = { Text(stringResource(R.string.change_server)) }, text = { Text(stringResource(R.string.change_server_description)) },
            confirmButton = { TextButton(onClick = { confirmServer = false; settings = false; host?.close(); host = null; model.disconnect { done -> WorkspaceHost.clearData(done) } }) { Text(stringResource(R.string.change_server)) } },
            dismissButton = { TextButton(onClick = { confirmServer = false }) { Text(stringResource(R.string.cancel)) } })
        model.export?.let { file ->
            AlertDialog(onDismissRequest = { model.dismissExport() }, title = { Text(stringResource(R.string.save_file)) }, text = { Text(file.name) },
                confirmButton = { TextButton(onClick = {
                    try { save.launch(Intent(Intent.ACTION_CREATE_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType(file.mime).putExtra(Intent.EXTRA_TITLE, file.name)) }
                    catch (_: Exception) { model.error = R.string.download_failed; model.dismissExport() }
                }) { Text(stringResource(R.string.save)) } }, dismissButton = { TextButton(onClick = { model.dismissExport() }) { Text(stringResource(R.string.cancel)) } })
        }
    }
    @Composable private fun Connect(model: SessionModel) {
        var address by remember { mutableStateOf("") }
        Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing).imePadding().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            Column(Modifier.widthIn(max = 480.dp), verticalArrangement = Arrangement.spacedBy(20.dp)) {
                Text("Lester", style = MaterialTheme.typography.displaySmall)
                Text("Make it happen.", style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(stringResource(R.string.connect_description), color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(value = address, onValueChange = { address = it }, label = { Text(stringResource(R.string.server_address)) }, placeholder = { Text("https://lester.example") }, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), singleLine = true, modifier = Modifier.fillMaxWidth())
                model.error?.let { Text(stringResource(it), color = MaterialTheme.colorScheme.error) }
                Button(onClick = { model.connect(address) }, enabled = !model.clearingData && address.isNotBlank(), modifier = Modifier.fillMaxWidth(), shape = RoundedCornerShape(16.dp)) { Text(stringResource(R.string.connect)) }
            }
        }
    }
    @Composable private fun Recent(model: SessionModel, select: (String) -> Unit) {
        var query by remember { mutableStateOf("") }
        Column(Modifier.fillMaxHeight().windowInsetsPadding(WindowInsets.safeDrawing).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("Lester", style = MaterialTheme.typography.titleLarge)
            OutlinedTextField(value = query, onValueChange = { query = it }, singleLine = true, label = { Text(stringResource(R.string.search)) }, modifier = Modifier.fillMaxWidth())
            Text(stringResource(R.string.recent), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            val items = model.workspace?.conversations.orEmpty().filter { query.isBlank() || it.title.contains(query, true) || it.projectName.contains(query, true) }
            if (items.isEmpty()) Text(stringResource(if (query.isBlank()) R.string.empty_conversations else R.string.no_matches), style = MaterialTheme.typography.bodySmall)
            LazyColumn(Modifier.weight(1f)) {
                items(items, key = { it.id }) { item ->
                    NavigationDrawerItem(selected = item.id == model.workspace?.currentId, onClick = { select(item.id) }, label = {
                        Column(Modifier.padding(vertical = 6.dp)) {
                            Text(item.title, maxLines = 2, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium)
                            Text(item.projectName, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    })
                }
            }
        }
    }
    @Composable private fun NativeIcon(icon: Int, label: Int, action: () -> Unit) { IconButton(onClick = action) { Icon(painterResource(icon), contentDescription = stringResource(label)) } }
}

@Composable private fun LesterTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val colors = if (dark) darkColorScheme(primary = Color(0xFFAACFB1), surface = Color(0xFF111612)) else lightColorScheme(primary = Color(0xFF41624C), surface = Color(0xFFFAFCF9), surfaceContainerLow = Color(0xFFF0F4EE))
    MaterialTheme(colorScheme = colors, content = content)
}
