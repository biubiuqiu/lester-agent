# Getting started

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/getting-started.md)

Install Lester and run your first task.

## In this chapter

- [Quick start](#quick-start)

## Quick start

### Prerequisites

- Docker and Docker Compose v2
- Credentials for a supported model provider

### 1. Configure a fresh installation

```bash
cp deploy/.env.example deploy/.env
```

Fill in `POSTGRES_PASSWORD`, `MASTER_KEY_BASE64`, `SANDBOX_SERVICE_TOKEN`, and `MINIO_ROOT_PASSWORD`. The example intentionally contains no usable passwords or keys. Generate independent values with:

```bash
openssl rand -hex 24       # POSTGRES_PASSWORD
openssl rand -base64 32    # MASTER_KEY_BASE64
openssl rand -hex 32       # SANDBOX_SERVICE_TOKEN
openssl rand -hex 24       # MINIO_ROOT_PASSWORD
```

For an existing installation, preserve your current `.env`, encryption key, and persistent volumes. Do not overwrite them with the example.

### 2. Start Lester

Run from the repository root:

```bash
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.yaml \
  up -d --build
```

PowerShell equivalent:

```powershell
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build
```

To restart an existing installation without rebuilding, omit `--build`.

### 3. Start your first conversation

1. Open [http://localhost:13000](http://localhost:13000), then select **进入工作区** to enter `/app`.
2. Register and sign in. Lester creates your Personal Workspace automatically.
3. Select **配置第一个模型** on the home screen (or open **Settings → Models**). Save your provider connection, then add a Model ID. Connection and display names are optional; advanced endpoint/JSON configuration remains available. The first model is selected as your personal default unless you uncheck that option.
4. Type a goal in the home composer. Sending the first message creates the conversation and starts the task.
5. Open a resulting file card to preview its contents, inspect source, download it, or reference it in a follow-up message.

The setup page returns you to the original project with your message draft preserved. Saving configuration does not verify provider access; the first task confirms whether the model can be called. Home-screen task examples only fill the composer and never send automatically. On phones, the conversation's **…** menu contains model switching and Published sites, while **文件** opens the file panel. Assistant file names displayed as inline code can open the preview when they match a file in the current inventory.

If the default port is occupied or reserved by Windows, choose an available port and update **both** settings in `deploy/.env`:

```dotenv
GATEWAY_PORT=13180
WEB_ORIGIN=http://localhost:13180
```

Restart Compose and open [http://localhost:13180/app](http://localhost:13180/app). The repository default remains port `13000`; the illustrative walkthrough uses an isolated browser fixture server.

Check service status with:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml ps
```

---

[User guide →](usage.md)
