# Lester for Android

A Kotlin / Jetpack Compose client for your own Lester server. **Make it happen.**

Supports **Android 8.0+ (API 26), phones and tablets**. Uses Material 3 navigation and the existing authenticated Web workspace. The server must have migrations **001–034** and matching API/Web; Android reuses the iOS handoff endpoints and adds no migration. A recent Android System WebView / Chrome with AndroidX WebMessageListener and document-start injection support is required. The App asks for an update instead of installing an unsafe legacy JavaScript interface.

## Build and install

Install **JDK 21**, Android SDK **36**, Build-Tools **36.0.0** and Platform-Tools through Android Studio / command-line tools. Set `ANDROID_HOME` to the SDK directory (or use an ignored `local.properties`). The checked-in Gradle **8.14.3** wrapper, AGP **8.13.2**, Kotlin **2.2.21**, dependency locks and SHA-256 verification metadata pin the build.

```bash
cd android
./gradlew --no-daemon :core:test :app:lint :app:assembleDebug :app:assembleRelease
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

On Windows use `gradlew.bat`. The default Debug build uses the normal generated development keystore. `app/build/outputs/apk/release/app-release-unsigned.apk` is **unsigned**; add your own signing configuration privately when distributing. Do not commit keystores, passwords, local SDK paths, build output or machine proxy/CA configuration. This project has no Play publishing credentials or Google Play release. CI retains build APKs/reports as the `android-build` artifact for seven days on Android/full runs.

Enter your server's **HTTPS origin**, e.g. `https://lester.example`, and sign in in a system browser / Custom Tab. Confirm the account with **Sign in to App**; the browser returns via `lester://auth/callback`. Email and configured Google/GitHub reuse existing server configuration. No provider secret belongs in the APK. Link new providers on the Web; already linked providers work for App sign-in.

For a local **Debug-only** stack, `adb reverse tcp:13000 tcp:13000` allows `http://localhost:13000` on an attached device/emulator while preserving that `WEB_ORIGIN`. Alternatively the emulator may use `http://10.0.2.2:13000`, but the deployment's `WEB_ORIGIN` must match it. Release permits HTTPS only, with system certificate validation. Debug HTTP is limited to localhost, 127.0.0.1 and 10.0.2.2; there is no trust-all TLS code or arbitrary cleartext exception.

## Included

- Phone conversation drawer and a sidebar on wide tablets, with recent conversations, current project names and search. Window resizing retains a single WebView and local selected files. System Back dismisses an open Web dialog before navigating history.
- Chat, attachments, files, isolated HTML previews/source, results, approvals, questions, queue and scheduled-task settings through the existing Web UI. Terminal/desktop tools are hidden only in the authenticated native shell. A URL parameter/user-agent never enables it in normal browsers.
- Six languages: Chinese, English, Japanese, Korean, French and Spanish. Native chrome follows the Web workspace's language after metadata arrives; language changes do not restart WebView or send tasks. All locales are included if you later build an App Bundle.
- Optional short completion vibration on supported hardware while the workspace is in the foreground. History, duplicate events and completion replay after returning never produce delayed vibration. Devices without a vibrator simply skip it.
- Persistent app-local Web drafts and HttpOnly cookies; existing two-hour access and rolling thirty-day refresh sessions remain unchanged. Changing server explicitly clears this device's cookies/Web data/drafts while preserving server tasks/files. App data is excluded from cloud backup and device transfer.
- Optional voice input uses the shared Web recorder and the server’s configured transcription service. The App requests microphone permission only for audio capture from the configured workspace origin while foregrounded; generation/account/page checks reject stale permission replies. Android’s permission callback identifies origins, not frames, so native origin checks are combined with document-start subframe guards and Web Permissions Policy/opaque previews. Backgrounding, navigation, logout and closing stop tracked capture; this adds no background recording service. Transcribed text remains an editable draft and is never automatically sent.
- System document chooser for explicit uploads, with no storage/photo/video/camera permission. Blob downloads use native-created expiring receipts, ordered chunks and exact-length checks; same-origin HTTP downloads reject redirects and enforce the same **50 MiB** cap. Files go to private temporary cache until the user chooses a destination through Android's create-document picker. Cancellation, failure and server changes discard pending exports; stale files are cleaned on startup.

**Not included:** FCM, lock-screen/background notifications, foreground services, background Agent execution on the phone, or Play publication. Server tasks continue when the App is closed; returning reloads authoritative state and the existing single SSE stream. Native Material 3 is used on Android; Apple's Liquid Glass remains iOS-only.

## Login and bridge boundaries

The system browser uses the same **single-use, 90-second S256 PKCE handoff** as iOS. The verifier/state are random, expire locally after ten minutes and survive process death only as Android Keystore AES-GCM encrypted data. The callback checks exact scheme/host/path, one code, matching state, no port/fragment or extra query fields. It is consumed locally before a single non-redirecting exchange; uncertain results require a fresh sign-in. Access/refresh values stay in WebView CookieManager as validated HttpOnly cookies, never in callback URLs, ordinary bridge messages, preferences or logs. Android's custom scheme is shared intentionally; another app intercepting a code lacks the private verifier. Production app-link ownership can be added separately for a managed public deployment.

WebMessageListener has an exact configured-origin allowlist, main-frame and current app-path checks, bounded JSON and protocol version validation. Sandboxed generated result frames and preview documents have no native authority. Native-to-Web commands navigate known routes only; they never send, approve, stop, execute or publish. File export messages also require the pending native download's unguessable receipt and do not grant arbitrary filesystem paths or network access.

## Validation

```bash
python3 android/scripts/check-resources.py
cd android
./gradlew --no-daemon :core:test :app:lint :app:assembleDebug :app:assembleRelease
```

JVM tests cover origins, subframe/preview exclusion, PKCE callbacks, foreground/deduplicated completion feedback and bounded export receipts. The resource check verifies six-language copy, manifest permissions, HTTPS/backup rules. Android lint and both APK builds check Android SDK usage and packaging. Web production Playwright fixtures exercise Android JSON and iOS WebKit bridge transports, normal browser behavior, drafts, file previews, foreground events and six-language help. These fixtures do **not** execute Android WebView or real Google/GitHub authorization.

Before distributing, verify on Android phones/tablets: microphone permission grant/deny/revoke, recording cancellation and audio format support, browser return and refresh-cookie renewal, keyboard/insets, rotation/multi-window, file upload/save, isolated HTML previews, Back, background return and physical vibration. No emulator/device run, live OAuth/SMTP or physical haptic validation should be inferred from successful JVM/APK/browser checks. Release signing belongs to your deployment; this repository does not upload to an app store.

## 中文说明

用 Android Studio 打开 `android/`，或配置 JDK 21、SDK 36 后运行上面的 Gradle Wrapper 命令。Debug APK 可直接安装测试，Release APK 需自己签名。连接已完成迁移 001–034 的 HTTPS Lester，邮箱、Google、GitHub 通过系统浏览器登录。支持 Android 8.0+ 手机和平板；前台完成短震可关闭，后台和锁屏推送暂缓。终端隐藏，保留聊天、成果、审批、提问、队列和定时任务。构建和 JVM/Web 检查不等于真机验收，发布前请验证登录返回、输入法、麦克风权限与录音格式、取消录音、文件选择、旋转/分屏和实际震动。

Home opens the task composer. The fixed Lester entry opens the independent personal assistant and memory panel, available across task/project/settings navigation. Independent execution tasks remain reachable through projects and task cards. The assistant cannot execute commands, edit files or approve operations.

语音输入复用网页版录音与后台配置的转写服务；管理员启用后，授权麦克风即可录音，识别文字进入可编辑草稿。不会自动发送、排队或执行 Agent 操作，也不提供后台录音。HTTPS 必需；Debug 的 localhost 可按浏览器安全上下文规则使用，模拟器的普通 HTTP 地址不保证支持麦克风。
