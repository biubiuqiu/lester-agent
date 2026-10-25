# 快速开始

[文档目录](README.md) · [项目首页](../../README.zh-CN.md) · [English](../getting-started.md)

安装 Lester 并开始第一个任务。

## 本章目录

- [快速启动](#快速启动)

## 快速启动

### 环境要求

- Docker
- Docker Compose v2

### 1. 准备配置

```bash
cp deploy/.env.example deploy/.env
```

`deploy/.env.example` 不再内置任何密码或密钥。复制后必须填写 `POSTGRES_PASSWORD`、`MASTER_KEY_BASE64`、`SANDBOX_SERVICE_TOKEN` 和 `MINIO_ROOT_PASSWORD`，可以分别生成：

```bash
openssl rand -hex 24       # POSTGRES_PASSWORD
openssl rand -base64 32    # MASTER_KEY_BASE64
openssl rand -hex 32       # SANDBOX_SERVICE_TOKEN
openssl rand -hex 24       # MINIO_ROOT_PASSWORD
```

### 2. 启动 Lester

```bash
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.yaml \
  up --build
```

### 3. 开始使用

1. 打开 <http://localhost:13000>，点击“进入工作区”前往 `/app`
2. 注册账号并登录
3. 点击首页的 **配置第一个模型**，或前往 **Settings → Models**。先保存服务商连接，再填写 Model ID；名称可以留空，高级设置仍支持自定义地址与 JSON 参数。首个模型默认勾选为个人默认模型，也可以取消。
4. 在首页输入目标，发送第一条消息即可开始与 Lester 的会话

配置完成后可返回原项目，继续使用已保留的消息草稿。保存配置并不验证服务商访问权限，首次任务才会确认模型是否可调用。首页任务示例只填入输入框，不会自动发送。手机会话页的 **…** 菜单提供模型切换和已发布站点，**成果** 按钮打开成果面板，文件可从面板标签或更多菜单查看。助手消息中以行内代码显示的文件名，匹配当前会话文件清单时可直接点击预览。

---

[使用指南 →](usage.md)
