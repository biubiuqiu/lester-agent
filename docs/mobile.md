# Mobile clients

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/mobile.md)

iPhone, iPad and Android setup and support boundaries.

> Commands run from the repository root unless stated otherwise. For existing databases, follow the [complete upgrade checklist](deployment.md#upgrading-an-existing-installation); feature-specific migration examples are partial steps.

## In this chapter

- [iPhone and iPad](#iphone-and-ipad)
- [Android phones and tablets](#android-phones-and-tablets)

## iPhone and iPad

The SwiftUI client in [`ios/`](../ios/README.md) connects to your own HTTPS deployment on iOS / iPadOS 18+. It provides adaptive native navigation, recent conversations with project labels, iOS 26 Liquid Glass (native fallback on older versions), and optional foreground completion haptics on supported hardware. Chat, results, approvals, queues and scheduled tasks reuse the existing workspace; Terminal and desktop tool entries are hidden. Sign-in uses the system browser and a single-use PKCE handoff, preserving existing email/Google/GitHub and access/refresh sessions.

Build and sign with **Xcode 26+**; no public App Store release is available yet. Background/lock-screen push is deferred. Existing servers must back up, stop API writes, apply **030** once after 001–029, then rebuild API and Web together. Fresh Compose includes 030; Helm applies SQL externally. Rollback removes pending App sign-in handoffs only. See the [setup, architecture and validation limits](../ios/README.md).

## Android phones and tablets

The Kotlin/Jetpack Compose client in [`android/`](../android/README.md) supports **Android 8.0+**, adaptive Material 3 navigation and optional foreground completion haptics. It reuses the Web workspace and iOS PKCE browser handoff, with chat, files/results, questions, approvals and queues; Terminal and desktop tool entries stay hidden. Uploads and downloads use system file pickers. No broad storage permission, FCM, background service or Google Play release is included.

Build with **JDK 21, SDK 36 and the checked-in Gradle Wrapper**. Existing servers need migration **030** and matching API/Web; Android adds no additional SQL. The Debug APK is installable for testing, while the Release APK requires your own signing configuration. CI runs JVM tests, Android lint and Debug/Release builds for Android changes and retains APK artifacts. See [build, install and validation limits](../android/README.md).

---

[← Development and CI](development.md) · [Walkthrough →](walkthrough.md)
