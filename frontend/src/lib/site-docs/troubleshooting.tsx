import type { SiteDocSection } from "./types";
import { T } from "@/components/i18n";
import Link from "next/link";
import { CodeBlock } from "@/components/site/code-block";
import { repositoryURL } from "../site";

export const sections: SiteDocSection[] = [
{ id: "cannot-start", title: "页面打不开或服务启动失败", content: <>
        <p><T>{"先检查服务状态和日志，确认四个必填密钥已配置。端口不可用时，同时更新"}</T><code>GATEWAY_PORT</code> <T>{"与"}</T><code>WEB_ORIGIN</code><T>{"。"}</T></p>
        <CodeBlock label="服务状态与日志">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml ps\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml logs --tail=100 api web gateway"}</CodeBlock>
        <p><T>{"新版源码需要重新构建运行容器；已有数据库需要显式补齐迁移，见"}</T><Link href="/docs/deployment#upgrade"><T>{"部署与升级"}</T></Link><T>{"。"}</T></p>
      </> },
{ id: "model-error", title: "模型已保存，任务仍报错", content: <>
        <p><T>{"配置保存和当前连接测试不会确认真实 Provider 调用。检查 Model ID、凭证权限、Endpoint 及部署环境的网络，再发一个小任务验证。"}</T></p>
        <p><T>{"没有可用模型时，先添加个人模型，或让管理员配置启用的共享模型。配置页返回原项目时会保留任务草稿。"}</T></p>
      </> },
{ id: "missing-file", title: "文件已生成，成果卡片没有出现", content: <>
        <p><T>{"运行中成果区会等待任务结束。结束后可刷新文件清单与成果；成果区只显示已同步、确认存在的 HTML/Markdown 文件。"}</T></p>
        <p><T>{"大目录或部分读取失败会限制同步范围。依赖、缓存和隐藏目录不会自动遍历，辅助文件也可能被类型发现规则排除。可以在文件面板手动查找，或让 Agent 明确登记真正的主成果。"}</T></p>
      </> },
{ id: "preview", title: "HTML 预览与普通浏览器行为不同", content: <>
        <p><T>{"私有 HTML 在受限 iframe 中执行。页面不能访问 Lester 的应用存储，网络请求、弹窗、表单和部分浏览器功能会受限制。相对链接只会打开同一会话清单中确认存在的文件。"}</T></p>
        <p><T>{"检查源码、依赖文件和生成页面自身的脚本错误。需要外部分享时，明确发布静态站点，而不是发送私有预览地址。"}</T></p>
      </> },
{ id: "capabilities", title: "现在支持记忆或定时任务吗？", content: <>
        <p><T>{"当前支持个人工作区、模型与工具调用、持久 Computer、Skills、自定义 Agent、成果预览及明确发布。上下文库需要手动选择条目。"}</T></p>
        <p><T>{"当前支持个人助理记忆、定时任务、通用入站 Webhook 和五种应用连接。应用默认用于搜索和读取；GitHub 可另外授权代码任务，在沙箱中修改、测试并推送分支或创建 PR。个人助理负责记忆与安排，具体工作由独立任务 Agent 执行。任务 Agent 也可使用 Computer 中已安装的 Playwright，没有专用浏览器界面。执行器仍在 API 内，尚无独立 Worker；中断后不会自动重放工具。"}</T></p>
      </> },
{ id: "report", title: "如何反馈问题或参与开发？", content: <>
        <p><T>{"前往"}</T><a href={`${repositoryURL}/issues`} target="_blank" rel="noopener noreferrer">GitHub Issues</a><T>{"，附上版本或提交号、复现步骤、预期行为和实际结果。日志请移除凭证及个人敏感内容。"}</T></p>
        <p><T>{"源码、架构说明和开发检查命令都在"}</T><a href={repositoryURL} target="_blank" rel="noopener noreferrer"><T>{"项目仓库"}</T></a><T>{"。欢迎提交问题和改进建议。"}</T></p>
      </> }
];
