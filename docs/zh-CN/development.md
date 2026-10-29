# 开发与 CI

[文档目录](README.md) · [项目首页](../../README.zh-CN.md) · [English](../development.md)

仓库结构、本地检查与 GitHub Actions。

## 本章目录

- [仓库结构](#仓库结构)
- [开发检查](#开发检查)
- [GitHub CI](#github-ci)

## 仓库结构

```text
lester-agent/
├── frontend/                     Next.js 前端工程
│   ├── src/
│   └── Dockerfile
├── backend/                      Go 后端工程
│   ├── cmd/api/                  API 服务入口
│   ├── cmd/sandbox-service/      Sandbox 服务入口
│   ├── cmd/lester-toolbox/       注入 Computer 的静态文件 Helper
│   ├── internal/                 后端内部实现
│   │   ├── agenttool/            工具注册表与独立工具 Handler
│   │   ├── toolboxfs/            Helper 的安全文件操作与协议
│   │   └── model/                模型存储、运行时契约与 Provider 集成
│   ├── prompts/                  Agent 系统 Prompt
│   ├── migrations/               PostgreSQL 迁移
│   ├── Dockerfile.api
│   ├── Dockerfile.sandbox-runtime
│   └── Dockerfile.sandbox-service
├── deploy/                       Docker Compose、环境配置与 Helm Chart
├── AGENTS.md                     编码 Agent 的开发约束
├── Makefile
├── docs/                         分章文档、中文目录与演示素材
└── README.md
```

这是一个 Monorepo，但前端和后端拥有独立的依赖、构建上下文与 Dockerfile。API 和 Sandbox Service 同属 Go 后端工程，但会编译成两个可执行程序并运行在两个容器中。

Agent 工具通过注册表扩展，每个工具独立维护参数 Schema 和执行逻辑；模型 Provider 通过 `internal/model/integration.Provider` 注册，数据库 Store 与对话运行时不包含具体 Provider 分支。详细扩展约束见 [`backend/ARCHITECTURE.md`](../../backend/ARCHITECTURE.md)。

Agent Runtime 默认不设置全局模型最大输出长度。OpenAI-compatible Provider（包括 DeepSeek 等）在未显式配置时不会发送 `max_tokens`，由模型服务按自身能力决定；Anthropic、Vertex Anthropic 和 Bedrock Anthropic 等协议强制要求输出上限的 Provider，会由各自适配器提供协议级兜底值。

## 开发检查

后端：

```bash
cd backend
go mod tidy
go test ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

前端：

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm test # 包含六语言文案完整性检查
pnpm lint
pnpm build
# 前端依赖改动时同时执行：
pnpm audit --prod --audit-level=high
```

也可以在仓库根目录执行：

```bash
make test
make web-check
```

## GitHub CI

CI 在推送 `main`、提交 PR 和手动运行时触发。同一事件下的分支或 PR 有新提交时，取消旧的检查。纯文档改动跳过无关构建；新增未知路径或无法获取 Git 比较范围时执行全套。修改 CI 本身会用 actionlint 验证工作流并执行全套。

| 检查 | 触发范围 | 验证内容 |
| --- | --- | --- |
| 后端 | 后端改动 | 模块文件一致性、gofmt、go vet、不使用测试缓存的单元/PostgreSQL 集成测试、全部 `cmd` 构建 |
| 前端 | 前端改动 | 锁定依赖安装、测试、ESLint、生产构建与类型检查、桌面/手机 Chromium 回归 |
| iOS / iPadOS | 原生源码、资源、构建配置改动 | 六语言资源、Core Swift 测试、Xcode 26+ 无签名模拟器及设备构建 |
| Android | 原生源码、资源、构建配置改动 | 固定 SDK、六语言资源、JVM 测试、lint 与无签名 APK 构建 |
| 网关 / Compose | 网关、Compose、部署环境模板改动 | 配置解析、路由/Cookie、预览隔离、上传、SSE、WebSocket 模拟服务回归 |
| Helm | Chart 改动 | lint 与包含产物 Ingress 的模板渲染 |
| Go 依赖安全 | 后端改动及每周 | govulncheck 检查可调用的漏洞符号 |
| 沙盒运行镜像 | 运行时、Toolbox、Provider 改动 | 构建、依赖审计、离线非 root 浏览器与编译检查、持久卷和终端集成测试 |
| 前端依赖安全 | 包清单、锁文件、pnpm 配置改动及每周 | pnpm audit 检查生产依赖的 high/critical 漏洞 |

浏览器回归使用生产 standalone 构建和固定 REST/SSE 服务，不调用真实模型、语音服务、OAuth 或邮件服务。构建后，在 `frontend/` 运行 `pnpm exec playwright install chromium`，再运行 `pnpm test:e2e`。桌面和手机 Chromium 覆盖登录错误、明确发送首个任务、草稿保留、发送失败、实时多文件标签和隔离 HTML 预览；也检查六语言官网与全部帮助章节、禁用 JavaScript 时的翻译、新手教学，以及切换语言后的草稿和附件保留。失败不自动重试，CI 保留失败截图与 trace 七天。语音用例通过可控媒体与服务商模拟数据，覆盖后台配置、取消录音、草稿与队列保留、原请求回执恢复。CI 使用两个浏览器 worker，Web 任务预算为 25 分钟，可用 `pnpm test:e2e --workers=2` 复现。

安全扫描使用独立任务名，发现漏洞或扫描出错仍会失败，不会忽略错误。每周一 03:23 UTC（北京时间 11:23）即使没有代码改动也扫描依赖；Actions 的 **Run workflow** 可手动执行全套。固定 Ubuntu 24.04、Node 22，从 `backend/go.mod` 与 `frontend/package.json` 读取 Go/pnpm 版本。集成测试使用临时 PostgreSQL，不访问生产数据或真实模型服务凭据。CI 不会部署应用。

原生构建和安全检查会保留七天失败日志，并把有界的关键报错放进公开注释。Go 注释包含漏洞编号和修复版本，前端审计注释包含受影响范围和补丁版本；原生注释显示编译器、SDK 或 Gradle 报错，不再只剩退出码 1 或 65。构建与审计仍通过 Bash `pipefail` 保持失败状态，生成注释不会把失败变为成功。最新依赖修复见 [2026-10-29 CI 排查](../security-ci-2026-10-29.md)。

开启分支保护时建议将 **CI result** 设为必需检查：它接受按范围跳过的任务，但拒绝失败、取消或检查选择失败。已有必需检查名称可能需要与新版任务名对齐。工作流和范围选择可在本地验证：

```bash
python3 -m unittest discover -s .github/scripts -p 'test_*.py' -v
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
```

---

[← 账号、管理与安全](accounts-and-security.md) · [移动客户端 →](mobile.md)
