import type { SiteDocSection } from "./types";
import { T } from "@/components/i18n";
import Link from "next/link";
import { CodeBlock } from "@/components/site/code-block";
import { Note } from "./shared";
import { cloneCommand, startCommand } from "../site";

export const sections: SiteDocSection[] = [
{ id: "prerequisites", title: "准备好这些", content: <ul><li><T>{"Docker 与 Docker Compose v2。"}</T></li><li><T>{"支持的模型服务及其访问凭证。"}</T></li><li><T>{"Git，用于获取项目源码。"}</T></li></ul> },
{ id: "get-project", title: "1. 获取项目", content: <CodeBlock label="获取项目">{cloneCommand}</CodeBlock> },
{ id: "environment", title: "2. 配置环境", content: <>
        <CodeBlock label="创建环境配置">{"cp deploy/.env.example deploy/.env"}</CodeBlock>
        <p><T>{"编辑"}</T><code>deploy/.env</code><T>{"，填写数据库密码、加密密钥、Sandbox Service Token 和对象存储密码。下面的命令会生成四个独立的值，把每个输出填写到对应变量中："}</T></p>
        <CodeBlock label="生成独立密钥">{"openssl rand -hex 24     # POSTGRES_PASSWORD\nopenssl rand -base64 32  # MASTER_KEY_BASE64\nopenssl rand -hex 32     # SANDBOX_SERVICE_TOKEN\nopenssl rand -hex 24     # MINIO_ROOT_PASSWORD"}</CodeBlock>
        <p><T>{"这些命令适用于提供 OpenSSL 的终端。Windows 用户可使用 Git Bash 或 WSL 来生成密钥，再通过 PowerShell 运行 Docker Compose。"}</T></p>
        <Note><T>{"环境配置保存在"}</T><code>deploy/.env</code><T>{"，请勿提交到 Git。已有部署请保留原配置、加密密钥和数据卷，并阅读"}</T><Link href="/docs/deployment#upgrade"><T>{"升级说明"}</T></Link><T>{"。"}</T></Note>
      </> },
{ id: "start", title: "3. 启动服务", content: <>
        <CodeBlock label="启动 Docker Compose">{startCommand}</CodeBlock>
        <p><T>{"从仓库根目录运行。构建和启动完成后，打开"}</T><a href="http://localhost:13000" target="_blank" rel="noopener noreferrer">http://localhost:13000</a><T>{"。官网位于根路径，点击“进入工作区”前往"}</T><code>/app</code><T>{"。"}</T></p>
        <p><T>{"部署在远程主机时，请使用该主机配置的访问地址，而不是浏览器所在电脑的 localhost。"}</T></p>
      </> },
{ id: "first-task", title: "4. 完成第一项任务", content: <>
        <ol><li><T>{"进入工作区，使用邮箱或已配置的 Google / GitHub 注册、登录。启用邮件服务时先验证邮箱。Lester 会创建你的 Personal Workspace 和默认项目。详见"}</T><Link href="/docs/usage#account"><T>{"账号与登录"}</T></Link><T>{"。"}</T></li><li><T>{"点击“配置第一个模型”，保存服务连接，再添加对应的 Model ID。更多说明见"}</T><Link href="/docs/models"><T>{"模型配置"}</T></Link><T>{"。"}</T></li><li><T>{"输入一个目标，例如“制作一个产品介绍网页，保存为 index.html”。需要时上传材料，再明确发送。"}</T></li><li><T>{"跟随执行过程。文件写入或编辑后会自动打开预览；任务结束后还可从成果卡片下载文件或点击“继续修改”。"}</T></li></ol>
        <Note><T>{"保存模型配置不代表连接已经验证。第一次实际任务会确认模型是否能被调用；示例任务只会填写输入框，不会自动发送。"}</T></Note>
        <p><Link href="/app" prefetch={false}><T>{"进入当前部署的工作区"}</T></Link><T>{"，或继续阅读"}</T><Link href="/docs/usage"><T>{"使用指南"}</T></Link><T>{"。"}</T></p>
      </> }
];
