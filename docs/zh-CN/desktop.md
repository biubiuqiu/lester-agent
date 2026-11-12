# 桌面客户端与本地任务

[文档目录](README.md) · [项目首页](../../README.zh-CN.md) · [English](../desktop.md)

Electron 桌面客户端在同一侧栏展示云端与本地任务。新建任务时选择执行位置，之后该任务始终保留原执行主机。云端任务打开现有 Web 工作区；本地任务在设备上由你选择的项目目录执行，提供聊天、操作审批、文件查看、Git diff 与交互终端。

## 平台与启动

- **macOS：** 随应用打包的 Go Runner 使用原生 POSIX 进程与终端执行。
- **Windows：** Electron 外壳运行在 Windows，Linux Runner 通过 **WSL2** 执行。先安装 WSL2、初始化一个 Linux 发行版，确认 `wsl.exe --exec uname -s` 输出 `Linux`，再打开项目。请选择 Windows 盘符下的项目目录，Runner 会通过 `/mnt/<盘符>/...` 打开。暂不支持 UNC、网络目录或 `\\wsl$` 文件夹；工具使用默认发行版的 Linux 环境，当前不提供原生 Win32 任务执行。
- **Linux：** 可用于开发检查。若操作系统安全加密不可用，或 Electron 选择 `basic_text`，凭证只保留在本次进程内存中，退出后需重新填写。

本地任务不要求云端账号，也不要求安装本地 Docker/PostgreSQL。所选模型和项目工具需在设备上可用；远程模型接口仍需联网。命令使用当前操作系统用户的权限。项目选择限定文件工具范围，但不会为命令和终端建立操作系统沙箱；授权前请查看具体操作。

从源码运行需 Node.js 22 及 `backend/go.mod` 指定的 Go 版本：

```bash
cd desktop
npm ci
npm run build:runner
npm start
```

Windows 的 `build:runner` 生成供 WSL2 使用的 Linux Runner；macOS 使用本机架构。`npm run build:runner:all` 交叉编译 macOS/Linux 的 x64、arm64 Runner。`npm run pack`、`npm run dist:mac`、`npm run dist:win` 打包桌面外壳；正式分发仍需配置对应平台的签名与公证。构建成功不代表已经完成签名或真实设备测试。

## 模型与凭证

在桌面模型设置中添加设备模型，选择协议、接口地址、Model ID 并填写凭证。保存配置与真实服务调用分开。设备凭证由 Electron 的操作系统安全存储加密（macOS 使用 Keychain，Windows 使用系统保护存储）；Runner 仅通过主进程私有 IPC 接收凭证，不写入 SQLite，也不发送到云端目录。

登录自己的 Lester 部署后，本地任务也可选择有权使用的云端模型。推理经过限定范围的桌面设备网关，云端服务商密钥保留在服务器，本地工具仍在设备执行。推理所需的任务上下文（包括选入的文件和工具内容）会发送给所选模型。

云端推理失败会保留认证、限流和上下文超限等限定的错误类型，流开始前的失败也会传回运行器。界面不会展示服务商原始响应，失败或结果未知的任务不会自动重发。

设备模型的“共享给云端”默认关闭。显式开启后，仅发布该模型的元数据，云端任务通过认证后的设备 WebSocket 请求推理，由桌面客户端调用模型接口，服务商凭证仍留在设备。共享要求设备保持连接。撤销设备或关闭共享会移除这条路由，不会新建替代运行或改变任务主机。

## 存储、隐私与恢复

本地聊天正文、工具调用、工具结果和摘要检查点按会话保存在应用用户数据目录的 JSONL 文件中：`profiles/<profile>/contexts/<conversation-id>.jsonl`。项目、运行状态、审批、事件和原始提交回执仍保存在同一档案的 `workspace.sqlite` 中；SQLite 的事务待写记录保证 JSONL 写入与运行接收一致，成功同步后清除。项目文件仍在选定目录。切换服务器或账号使用独立档案，档案或设备身份不匹配时拒绝打开。

首次启动新版 Runner 会自动迁移旧 SQLite 聊天记录，保留消息 ID、顺序、部分回复及提交回执。长上下文使用无工具调用的模型请求生成经过验证的摘要，摘要只用于后续模型请求，完整原始记录不会被覆盖。备份和恢复须包含整个档案目录（SQLite 与 JSONL），升级后的档案不要交给旧版 Runner 打开。

每个档案同一时间只允许一个 Runner 写入。重启时仅从已提交的待写记录修复未完成的末行；已确认记录缺失、正文损坏或格式版本不受支持会明确报错，不会当成空会话继续。云端执行会话继续保存在后端 PostgreSQL。

登录云端后，私有目录只同步本地任务 ID、标题、项目标签、状态、时间及设备元数据，不上传聊天正文、项目路径和文件。即使同属工作区，其他成员也不能浏览此拥有者的本地目录元数据。

设备离线显示为离线，不代表任务失败；断开期间云端目录可能滞后。响应丢失或 Runner 重启后，应检查原始本地回执与持久状态；中断或结果未知的操作不会自动重放。不提供自动续跑、云端/本地任务迁移、文件同步或聊天转移。核对实际结果后，再明确停止或继续工作。

## 服务端升级与检查

备份 PostgreSQL、对象存储和 Computer 数据，停止 API 写入与活跃任务，按顺序执行尚未应用的全部迁移至 `000038_memory_management.up.sql`。保留 `.env`、`MASTER_KEY_BASE64` 与数据卷，同步重建 API/Web。新 Compose 挂载 **001–038**；已有卷不会重新执行初始化 SQL，Helm 需单独执行 SQL。详见[升级检查](deployment.md#已有部署升级检查)。

037 新增设备身份、派生共享模型定义及仅含元数据的本地任务目录。回滚移除这些目录和模型路由，保留云端历史消息及设备本地文件/SQLite。回滚前先断开桌面客户端并配套兼容 API；回滚不撤销任务或服务商已发生的操作。

```bash
cd desktop
npm test
npm run check
npm run build:runner:all
npm run test:e2e
```

专项 CI 覆盖外壳单元/静态检查、共享 Harness/本地 Runner 测试和 Runner 交叉编译。模拟检查不代表真实模型服务访问、macOS/Windows 签名或真实 WSL2/原生终端已通过验收；发布前仍需在目标系统验证。

`test:e2e` 使用本机 Electron、真实 Runner、项目目录和终端，以及固定的模型服务测试数据；不会访问真实模型账号。Linux 需图形显示环境，例如 `xvfb-run -a npm run test:e2e`。仅在隔离的容器测试环境需要禁用 Electron 沙箱时设置 `LESTER_ELECTRON_NO_SANDBOX=1`，普通应用启动不使用此设置。截图保存在临时目录，可用 `LESTER_DESKTOP_ARTIFACTS` 指定位置。

双向模型链路也有可选的真实 PostgreSQL 系统测试：安装桌面依赖后，在 `backend` 中设置测试数据库 `LESTER_TEST_DATABASE_URL` 和 `LESTER_TEST_HYBRID_SYSTEM=1`，运行 `go test -race ./internal/desktopcloud -run '^TestHybridDesktopSystem$' -count=1 -timeout=2m`。它使用固定服务数据检验真实 WebSocket、网关、stdio 和本地工具链路。
