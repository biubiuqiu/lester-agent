# Lester Research

独立的 Agent 研究目录。每个专题同时保留可阅读的报告、研究笔记、固定版本的数据与可重建的展示源。研究结论有各自的版本和证据边界，不等同于 Lester 的产品功能承诺。

| 专题 | 阅读与资料 |
| --- | --- |
| Agent 上下文管理：Claude Code、Codex、OpenCode、Hermes、Pi | [交互 HTML](agent-context-management/index.html) · [研究包说明](agent-context-management/README.md) · [五篇笔记](agent-context-management/notes/) |

GitHub 文件页会显示 HTML 源码；下载后可直接用浏览器打开，或通过下面的本地服务器阅读。

```sh
python3 -m http.server 8080 --directory research
```

打开 <http://localhost:8080/>。单文件报告的图解、数据、样式与交互均内嵌，无需 npm、数据库或模型凭证。

## 检查和维护

```sh
python3 research/agent-context-management/scripts/build.py --check
```

修改专题的 `notes/`、`data/`、`assets/` 或生成器后，先运行不带 `--check` 的构建，再检查并一同提交产物。默认检查离线进行；本地另有固定源码 checkout 时，可按专题说明使用 `--source-root` 做额外实体验证。包内不包含第三方完整源码、运行日志或部署凭证。

`docs/research/agent-context-management.html` 保留原来的单文件快照供旧引用使用；后续维护以本目录为准。仓库原有的 [Agent 记忆设计研究](../docs/research/agent-memory-design.md) 继续保留在原位置。

## GitHub Pages

[Research Pages 工作流](../.github/workflows/research-pages.yml) 负责验证和部署。主分支的研究内容更新会自动触发，PR 只验证而不发布，也可在 Actions 手动运行。

首次发布需要仓库管理员在 [Settings → Pages](https://github.com/biubiuqiu/lester-agent/settings/pages) 把 **Build and deployment → Source** 设为 **GitHub Actions**。这是 GitHub 仓库的一次性配置，普通工作流的 `GITHUB_TOKEN` 不能代替管理员首次启用 Pages。之后可到 [Research Pages](https://github.com/biubiuqiu/lester-agent/actions/workflows/research-pages.yml) 运行或重跑工作流；如果 `github-pages` 环境要求审核，则需要该环境的实际审核通过。

部署使用 `research/index.html` 作为站点首页，专题保留相对路径：

- 预期首页：<https://biubiuqiu.github.io/lester-agent/>
- 预期专题：<https://biubiuqiu.github.io/lester-agent/agent-context-management/>

是否已上线以 Actions 中 **Deploy research** 的结果与 `github-pages` 环境的部署链接为准。工作流不会自动更改 Pages 的源、自定义域名或环境保护规则；发现源不是 GitHub Actions 或已有不相关页面时会停止，避免覆盖其他站点。

Pages artifact 只包含阅读首页、各专题的最终 HTML 和 `.nojekyll`，由 `.github/scripts/build_research_site.py` 暂存。笔记、数据和生成器仍在 GitHub 目录中完整保留，并通过站内链接访问；不会上传工作区或第三方 checkout。

