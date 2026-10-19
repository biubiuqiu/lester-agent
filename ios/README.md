# Lester for iPhone and iPad

A SwiftUI client for your own Lester server. **Make it happen.**

Requires iOS / iPadOS 18+, **Xcode 26+** and an HTTPS Lester deployment with migration **030** and matching API/Web. This is source code to build and sign; there is no public App Store download yet.

## Run on a Mac

1. Upgrade your server using the root README. Preserve keys, accounts and volumes. Back up PostgreSQL, stop API writes and active tasks, apply all missing migrations through `backend/migrations/000031_personal_assistant.up.sql`, and rebuild API and Web together. Fresh Compose databases include it automatically; Helm upgrades apply SQL externally.
2. Open `ios/Lester.xcodeproj` in Xcode 26+. The shared scheme is **Lester**. The only package is the local `Packages/LesterCore`; no third-party native dependencies or project generator are needed.
3. Select your signing Team and a unique bundle identifier in the app target. Choose an iPhone or iPad simulator, or a provisioned device, and Run. Enter the server’s HTTPS origin, such as `https://lester.example` (deployment at the domain root).
4. Sign in in the system authentication browser using your configured email, Google or GitHub account. Confirm the displayed account with **Sign in to App** and return to Lester. OAuth callbacks remain the existing server-owned HTTPS URLs; there are no mobile provider secrets to embed.

Release enforces HTTPS with normal system certificate validation. Debug supports `http://localhost:13000` for a Mac-hosted stack in Simulator only. A phone’s `localhost` is the phone, so physical devices need a reachable HTTPS server. Do not disable ATS or certificate checks. Existing providers/SMTP still require the deployment’s real configuration. Third-party account linking is performed in the Web profile; already linked providers work for App sign-in.

## Experience

- Native adaptive navigation: iPhone starts in the conversation; iPad provides a sidebar with recent conversations, project labels and search. iPad portrait, landscape and split-screen are supported by SwiftUI’s adaptive columns.
- Existing chat, attachments, approvals, questions, queues, files, HTML preview/source and scheduled tasks use the server’s Web UI. The App hides Terminal and the desktop tools menu. Cloud execution remains unchanged.
- iOS 26 uses the system Liquid Glass button style and native navigation chrome. Earlier supported versions fall back to native bordered controls/materials. Light/dark appearance follows the system for the native shell.
- One gentle completion haptic when a **newly observed running task finishes while the workspace is in the foreground**. Settings can disable it. Core Haptics capability detection skips unsupported hardware, including iPads without haptics. There is no per-token vibration or delayed vibration for historical/replayed/background completions.
- Switching away does not stop server tasks. Returning refreshes existing workspace/conversation state and reconnects the single existing Web SSE stream. **No APNs, background execution entitlement, lock-screen notifications, Live Activities or background completion guarantees are included in this version.**
- Local drafts and cookies use the persistent WKWebsiteDataStore. Change server explicitly clears this device’s Web data and drafts, preserving server files/tasks. Server logout uses the existing session revocation behavior.
- User-triggered downloads from the trusted workspace use WebKit downloads and the iOS share sheet; announced file size must be known and at most 50 MiB. Concurrent downloads are bounded; cancelled/failed transfers are discarded. Generated preview frames cannot initiate native actions.
- Native and Web UI support Chinese, English, Japanese, Korean, French and Spanish. Native chrome follows the current Web locale once workspace metadata arrives, otherwise the device locale.

## Authentication and bridge boundaries

`ASWebAuthenticationSession` handles login in a system browser, keeping Google/GitHub outside embedded WebView login restrictions. The browser grants a random **90-second, single-use handoff** only after explicit authenticated confirmation. The App exchanges it with its private S256 PKCE verifier. The server rechecks the account, membership and original live browser session under a user lock, consumes the handoff atomically, and creates normal two-hour access / rolling thirty-day refresh cookies. Only hashes are stored. Access/refresh values never enter callback URLs, JavaScript messages, UserDefaults or logs. A callback from another app cannot redeem a code without the verifier. No redirect URI is accepted from clients.

Only the configured server’s main `/app` or `/login` frame can deliver versioned native bridge messages. Generated `/preview` documents, sandboxed result iframes and other origins cannot. Messages contain bounded workspace/recents metadata and completion IDs, never chat/file contents or credentials. Native commands navigate to known routes; they cannot send tasks, approve permissions, execute, stop or publish. Server authorization stays authoritative. A query parameter does not enable App mode in regular browsers.

## Checks

On macOS:

```bash
python3 ios/scripts/check-resources.py
swift test --package-path ios/Packages/LesterCore --jobs 2
bash ios/scripts/check-build.sh
```

The build script builds unsigned Release simulator + device targets and the localhost-only Debug simulator configuration. CI selects the iOS job for `ios/**` changes, manual full runs or CI configuration changes; weekly dependency scans do not start macOS builds. The existing stable **CI result** includes the selected native job. These unsigned builds do not upload or sign an App Store/TestFlight release.

The Foundation-only bridge/origin/completion tests also run on Linux with Swift 6.2.1. Frontend production Playwright fixtures cover phone/tablet layout, explicit login handoff errors, draft recovery, one completion event and normal browser behavior. They do **not** validate UIKit/WebKit behavior, real OAuth/SMTP, hardware haptics or Liquid Glass rendering. Use Xcode and real devices to verify system-browser return, cookie renewal, camera/file selection, keyboard/safe areas, HTML isolation, download sharing and background/foreground behavior before distributing. App Store/TestFlight requires your Apple Developer signing/provisioning and a deployment-specific privacy disclosure; the bundled privacy manifest declares the App’s UserDefaults access, not every possible self-hosted server’s data policy.

## 简要说明

用 Xcode 26+ 打开 `ios/Lester.xcodeproj`，选择签名 Team 和设备后运行，连接自己的 HTTPS Lester。服务端需补齐迁移 001–031 并一起更新 API/Web。支持 iOS/iPadOS 18+；iOS 26 使用系统 Liquid Glass，旧版回退原生样式。iPad 自适应侧栏显示最近会话和所属项目；App 内隐藏终端等桌面入口，保留聊天、成果、审批、队列与定时任务。

首版只在工作区前台收到新任务完成时轻震一次，支持关闭和硬件检测；不支持的 iPad 不会震动。后台、锁屏 APNs 推送按本次范围暂缓。Linux 可验证核心逻辑和网页适配，原生构建、玻璃效果、登录返回和真机震动必须用 macOS/Xcode 与设备验收。这里提供源码工程，不代表已上架。

Home opens your enduring personal Lester dialogue and memory. Independent execution tasks remain reachable through projects and task cards. The assistant cannot execute commands, edit files or approve operations.
