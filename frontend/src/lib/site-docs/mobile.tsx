import type { SiteDocSection } from "./types";
import { T } from "@/components/i18n";

export const sections: SiteDocSection[] = [
{ id: "ios", title: "iPhone 与 iPad", content: <>
        <p><T>{"原生客户端连接你自己的 HTTPS Lester 部署，支持 iOS / iPadOS 18 及以上；需要使用 Xcode 26 及以上构建并配置签名。客户端源码和安装说明见仓库的 ios 目录，目前没有公开 App Store 下载。"}</T></p>
        <p><T>{"保留聊天、附件、成果预览、审批、任务队列与定时任务。原生侧栏展示最近会话及所属项目；终端和桌面工具入口在 App 内隐藏。iOS 26 使用系统 Liquid Glass，旧版使用原生材质回退。"}</T></p>
        <p><T>{"登录通过系统浏览器完成，沿用已配置的邮箱、Google 和 GitHub。第三方账号绑定在网页版操作；App 用一次性、短期且绑定 PKCE 的凭证交接登录，不会把访问或刷新令牌放入回调 URL。"}</T></p>
        <p><T>{"工作区在前台收到新任务完成事件时可轻震一次，可在 App 设置中关闭；仅支持具备触感硬件的设备，iPad 不保证震动。历史会话、重复事件和切回前台不会补震。后台与锁屏系统推送暂未接入，任务仍在服务器执行。"}</T></p>
        <p><T>{"已有部署需先备份、停止 API 写入并按顺序补齐迁移 001–036，再一起重建 API 和 Web。030 只新增 App 登录交接记录，回滚会使待完成的 App 登录失效，不删除账号、会话或文件。"}</T></p>
      </> },
{ id: "android", title: "Android 手机与平板", content: <>
        <p><T>{"Android 原生客户端支持 Android 8.0 及以上，连接你自己的 HTTPS Lester。使用原生 Material 3 导航，手机显示会话抽屉，宽屏平板显示最近会话侧栏和项目名；保留聊天、附件、成果、审批和队列，隐藏终端等桌面入口。"}</T></p>
        <p><T>{"邮箱、Google 和 GitHub 登录通过系统浏览器完成，使用与 iOS 相同的一次性 PKCE 交接。回到 App 后沿用 2 小时 access token 与滚动 30 天 refresh token。首次安装需要较新的 Android System WebView 或 Chrome；第三方账号绑定仍在网页版操作。"}</T></p>
        <p><T>{"前台工作区收到新任务完成时可短震一次，可关闭并检测硬件支持；历史、重复及后台完成事件不会补震。暂不包含 FCM 或锁屏推送，离开 App 后任务仍在服务器执行。文件通过系统选择器上传，下载通过系统保存窗口，不申请整个存储空间的权限。"}</T></p>
        <p><T>{"源码、构建与安装步骤位于仓库 android 目录；需要 JDK 21、Android SDK 36 和固定版本的 Gradle Wrapper。已有服务端需补齐迁移 001–036 并更新 API/Web，Android 不再新增迁移。Debug APK 可用于安装测试；Release APK 需自行签名，目前未发布到 Google Play。"}</T></p>
      </> }
];
