# 移动客户端

[文档目录](README.md) · [项目首页](../../README.zh-CN.md) · [English](../mobile.md)

iPhone、iPad、Android 的安装入口与支持范围。

> 除非代码块另有说明，命令均从仓库根目录运行。已有数据库先看[完整升级检查](deployment.md#已有部署升级检查)，功能章节中的迁移示例只是部分步骤。

## 本章目录

- [iPhone 与 iPad](#iphone-与-ipad)
- [Android 手机与平板](#android-手机与平板)

## iPhone 与 iPad

原生客户端位于 [`ios/`](../../ios/README.md)，连接自己的 HTTPS Lester，支持 iOS / iPadOS 18+。iPad 自适应侧栏展示最近会话和所属项目；iOS 26 使用系统 Liquid Glass，旧版回退原生样式。保留聊天、附件、成果、审批、队列与定时任务，App 内隐藏终端等桌面入口。登录通过系统浏览器复用邮箱、Google / GitHub，再用一次性 PKCE 凭证交回 App，沿用现有 access / refresh 会话。

首版只在工作区前台收到新任务完成时轻震一次，可关闭并检测硬件支持，iPad 不保证震动；后台与锁屏推送暂缓。需用 **Xcode 26+** 构建、签名和真机验收，目前没有公开 App Store 下载。已有服务端先备份、停止 API 写入，在 001–029 后执行一次 **030**，再同时重建 API/Web；全新 Compose 自动包含，Helm 手动迁移。030 回滚只使待完成的 App 登录交接失效。详见 [安装与验证范围](../../ios/README.md)。

## Android 手机与平板

[`android/`](../../android/README.md) 提供 Kotlin / Jetpack Compose 原生客户端，支持 **Android 8.0+**。手机使用会话抽屉，宽屏平板显示最近会话侧栏与项目名，采用 Material 3 原生风格。沿用现有聊天、成果、审批、提问、队列与 PKCE 系统浏览器登录；隐藏终端等桌面入口。支持可关闭的前台完成震动，文件通过系统选择和保存窗口操作，无需整个存储空间权限。首版不含 FCM、后台服务或锁屏推送。

使用 **JDK 21、SDK 36 和仓库内 Gradle Wrapper** 构建；服务端需执行至迁移 **030** 并更新 API/Web，Android 不新增 SQL。Debug APK 可安装测试，Release APK 需部署方自行签名，目前没有 Google Play 下载。CI 执行 JVM 测试、Android lint 与两种 APK 构建并保留产物。[安装说明和验证范围](../../android/README.md)。

---

[← 开发与 CI](development.md) · [操作演示 →](walkthrough.md)
