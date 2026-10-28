# Accounts, administration and security

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/accounts-and-security.md)

Registration, OAuth, tokens, avatars, admin access and security.

> Commands run from the repository root unless stated otherwise. For existing databases, follow the [complete upgrade checklist](deployment.md#upgrading-an-existing-installation); feature-specific migration examples are partial steps.

## In this chapter

- [Accounts and sign-in](#accounts-and-sign-in)
- [Administration](#administration)
- [Rotating login tokens, avatar cropping and standalone HTML preview](#rotating-login-tokens-avatar-cropping-and-standalone-html-preview)
- [Security notes](#security-notes)

## Accounts and sign-in

The login page supports email/password and configured Google/GitHub OAuth sign-in. First registration creates one Personal Workspace and default project; all self-registered users are members. Set `AUTH_REGISTRATION_ENABLED=false` to close **new** email and OAuth registrations; existing accounts can still sign in and link identities. Email/display-name/password lengths are validated on the server; registration includes password confirmation in the UI.

Configure OAuth in `deploy/.env` (leave an entire pair empty to hide that provider):

| Provider | Environment variables | Exact authorized callback |
| --- | --- | --- |
| Google | `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET` | `<WEB_ORIGIN>/api/v1/auth/oauth/google/callback` |
| GitHub | `GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET` | `<WEB_ORIGIN>/api/v1/auth/oauth/github/callback` |

Create a **Web application** OAuth client in Google Cloud Console (Google Auth Platform), configure its consent screen/test users or production availability, and register the exact callback. Create a GitHub **OAuth App** in Developer settings with this deployment's callback; use a separate app for each environment. OAuth callbacks must reach API through the same-origin gateway/Ingress. Use HTTPS and `SESSION_COOKIE_SECURE=true` in production; HTTP is supported only for localhost development. Keep secrets in `.env`/Kubernetes Secrets, never in frontend build variables. The API needs outbound HTTPS to the identity providers. Scopes are `openid email profile` for Google and `read:user user:email` for GitHub.

Identity ownership uses the provider's immutable subject/ID and a verified email (GitHub private emails are supported through `/user/emails`). Ten-minute state records are browser-bound, single-use and shared across API replicas; authorization uses PKCE S256. OAuth access tokens are used only during the callback and are not persisted. An email match with an existing Lester account **never automatically merges accounts**: sign in with the existing method, then bind from **个人资料 → 登录方式**. Binding does not replace the account email, name, projects or files. Unlinking requires another currently available login method or a password, rotates the current session and signs out other devices. Disabled accounts remain blocked. Configuration changes are enforced after API restart.

### Email verification and recovery

Optional SMTP enables verification for **new email registrations**, resending verification links and **忘记密码**. Without SMTP, the existing self-hosted immediate-registration behavior remains, with email marked unverified and mail controls hidden. Previously registered accounts are not suddenly locked out when enabling SMTP; they can verify from profile settings. Verification links last 24 hours, reset links 30 minutes; both are hashed at rest and single-use. Links carry tokens in URL fragments, which the login page immediately removes, and confirmation/submission is explicit. A reset verifies mailbox ownership and revokes all sessions and pending security tokens; setting/changing a password from settings preserves a freshly rotated current session and revokes others. Mail requests return the same accepted response for eligible/unknown accounts. Monitor API `account mail request failed` warnings if SMTP delivery is unavailable.

```dotenv
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_TLS_MODE=starttls
SMTP_USERNAME=your-mail-account
SMTP_PASSWORD=your-mail-password
SMTP_FROM=no-reply@example.com
```

Use a sender authorized by your mail service. `SMTP_TLS_MODE=tls` supports port 465; `starttls` requires server TLS support with valid certificates. `plain` is allowed only on localhost for development mail fixtures. In Helm, configure IDs/mail settings under `config.auth` and secrets under `secrets.googleOAuthClientSecret`, `secrets.githubOAuthClientSecret`, `secrets.smtpPassword`, or add `GOOGLE_OAUTH_CLIENT_SECRET`, `GITHUB_OAUTH_CLIENT_SECRET`, `SMTP_PASSWORD` to the referenced `existingSecret` when enabled. Pending unverified registrations still need their verification link; disabling SMTP does not grant them access. Mail delivery and provider consent must be checked with your deployment's real credentials.

### Avatars and account upgrade

Profile settings support PNG/JPEG/GIF uploads below 2 MiB, center-cropped and re-encoded to a 256×256 PNG (GIF first frame, metadata removed). Images exceeding 4096 pixels per side or 12 million pixels are rejected. Avatars use the existing private object store and authenticated `/api/v1/me/avatar`, with built-in initials as fallback. First OAuth registration attempts to import the provider avatar; failure does not block sign-in. Bound users can explicitly choose the provider photo, upload their own image or restore a built-in theme. Provider avatar fetches use an HTTPS hostname allowlist, strict size/time limits, no redirects and no access tokens. Replaced/deleted photo objects are removed on a best-effort basis; include avatar objects in object-store backups.

Accounts require migration **013** after 001–012; the latest version also requires **014** for user guides and **015** for rotating login tokens. For a database on 001–012, back up PostgreSQL/object storage, stop API writes, preserve your `.env` and encryption key, apply each **once**, then rebuild API/Web. If already on 013, apply [014 and 015](usage.md#step-by-step-user-guides):

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < backend/migrations/000013_account_identity.up.sql
# Apply 014–024 before restarting the latest services.
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < backend/migrations/000014_user_guides.up.sql
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < backend/migrations/000015_rotating_tokens.up.sql
for migration in 000016_run_submissions 000017_model_budget 000018_command_tasks 000019_model_request_usage 000020_context_checkpoints 000021_run_interactions 000022_run_inputs 000023_file_changes 000024_project_rules 000025_run_permissions 000026_cloud_task_indexes 000027_automations 000028_permission_reviews 000029_conversational_schedules 000030_native_signin 000031_personal_assistant; do
  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "backend/migrations/${migration}.up.sql" || exit 1
done
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web
```

Fresh Compose volumes initialize 001–034 automatically; existing volumes never re-run init scripts. PowerShell can pipe `Get-Content -Raw` into the same `exec -T postgres` command. Rolling back 013 removes identity/token/avatar references and therefore **refuses while accounts without passwords exist**; arrange password recovery before a downgrade and restore a compatible application version. Existing workspace files and projects are not moved. PostgreSQL-backed auth tests use mock identity providers and a local SMTP fixture; live third-party consent/delivery require deployment validation.

## Administration

Administrators have a separate console at `/admin/users` and `/admin/models`, accessible from the account menu. The console supports creating accounts with their own personal workspace and default project, editing names and roles, disabling/re-enabling accounts, and resetting passwords. Disabling an account, changing its role, or resetting its password revokes its sessions. Administrators cannot disable or demote themselves. An already-running agent request is not cancelled by disabling an account.

Shared models are configured centrally in **Administration → Shared models**. Add a provider connection and its encrypted credential, then add a model deployment. Connections and models can be edited; leaving the replacement credential empty retains the existing secret. A disabled model is removed from member selection and rejected for new requests, while historical conversations remain intact. Requests already started are unaffected. Shared connection credentials and provider configuration are never returned by member APIs.

Existing personal model configurations remain private and usable. A personal default takes priority over the system default. The administrator console manages only shared models; it does not expose other users' conversations, files, or personal credentials.

### Upgrade and bootstrap the first administrator

Back up PostgreSQL, stop the API, and apply `backend/migrations/000007_administration.up.sql` once after migrations 001–006. Fresh Compose installations apply it automatically. For an existing Compose installation:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api
cat backend/migrations/000007_administration.up.sql | docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web
```

In PowerShell use `Get-Content -Raw` instead of `cat`. Registration always creates a member account; the first signup does **not** automatically gain administrator privileges. Register the intended operator account, then use an authorized PostgreSQL session to bootstrap it explicitly (replace the example email):

```sql
UPDATE users SET role = 'admin', disabled = false
WHERE email = 'operator@example.com';
```

Verify exactly one row was updated, then refresh the app. Subsequent role changes are available in the console. Do not grant access by editing browser state; every admin API requires an authenticated, active administrator. The reserved system-model workspace has no user membership.

For the PostgreSQL-backed permission, account lifecycle, shared-model isolation, default rollback, and migration rollback checks, point `LESTER_TEST_DATABASE_URL` at a disposable PostgreSQL database and run `go test ./...` from `backend/`. Tests create and remove isolated schemas.

## Rotating login tokens, avatar cropping and standalone HTML preview

Login (email, Google and GitHub) issues separate opaque access and refresh credentials in HttpOnly/SameSite cookies; PostgreSQL stores only hashes. Access lasts **2 hours**. Refresh lasts **30 days**, rotates on use and restarts the 30-day window; continuously active sessions have no absolute lifetime. Thirty days without renewal requires a new login. The browser renews automatically before expiry and after an authentication-middleware rejection, coordinating concurrent requests/tabs without replaying domain mutations. Network outages preserve drafts. Logout and password/account security changes revoke the entire token family; refresh replay outside a five-second concurrent-tab grace revokes that device's family.

Avatar upload opens a crop dialog with drag/keyboard positioning, zoom, mobile pinch, rotation, reset and a circular preview. Confirm uploads the chosen 256px PNG; cancel leaves the avatar unchanged. Existing uploaded/provider photos can be cropped again from **Adjust crop**. HTML **Open in new tab** opens only the private, isolated HTML preview at `/preview/{conversationId}?path=...`, with authenticated local assets and HTML navigation; it does not publish or mount workspace panels.

For an existing database on **001–014**, back up, stop API writes, apply **015 once**, then rebuild API/Web. Older deployments must first apply all missing migrations in numeric order:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < backend/migrations/000015_rotating_tokens.up.sql
for migration in 000016_run_submissions 000017_model_budget 000018_command_tasks 000019_model_request_usage 000020_context_checkpoints 000021_run_interactions 000022_run_inputs 000023_file_changes 000024_project_rules 000025_run_permissions 000026_cloud_task_indexes 000027_automations 000028_permission_reviews 000029_conversational_schedules 000030_native_signin 000031_personal_assistant; do
  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "backend/migrations/${migration}.up.sql" || exit 1
done
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web
```

Migration 015 invalidates old sessions: users sign in once after upgrade. Accounts, profiles, projects and files remain intact. Retain `.env`, encryption keys and volumes. Fresh Compose volumes initialize 001–034; Helm requires SQL before deployment. Rolling back 015 also invalidates current credentials and must be paired with compatible API/Web. The lifetimes are fixed application defaults; no extra environment variables are required.

## Security notes

Security audit and fixes: [2026-10-10 report](security-audit-2026-10-10.md). Backend builds now require Go 1.26.9 for current security patches.

Behind a gateway/Ingress, set `AUTH_TRUSTED_PROXY_CIDRS` to the actual proxy peer CIDRs (comma-separated), or Helm `config.auth.trustedProxyCIDRs`. Inspect the gateway address/subnet with `docker network inspect <network-name>`; trust only the dedicated proxy IP (`/32` or `/128`) or its controlled subnet, update after a changed address, and restrict API access to that proxy. An empty value ignores forwarding headers, so users behind the same gateway share its IP rate bucket. Never trust `0.0.0.0/0`, `::/0`, arbitrary client headers, or unrelated workloads. Only `X-Forwarded-For` from a trusted peer is used; the supplied gateway replaces it and strips `True-Client-IP`.

Model endpoints must directly accept API requests: redirects are rejected to keep credentials and conversation content at the configured endpoint. Terminal messages are capped at 1 MiB. Open terminal/event connections recheck their session family every 30 seconds and close on logout, account disabling or workspace removal (up to another five seconds for a pending database check).

- Generate your own `MASTER_KEY_BASE64` and `SANDBOX_SERVICE_TOKEN`. Keep real secrets and local `.env` files out of Git.
- Model-provider credentials are encrypted at rest with AES-GCM.
- HTTPS deployments use Secure session cookies by default; sign-in and registration are rate-limited by identity and client IP; Redis failures deny rate-limited operations. Client IP defaults to the actual TCP peer. Refresh is limited to 60 attempts per minute by account and IP.
- Keep Sandbox Service private and Docker socket access on an isolated, dedicated worker.
- User Computers default to disabled networking and CPU, memory, and PID limits.
- File APIs and terminal working directories are conversation-scoped. HTML preview authentication, CSP, and iframe restrictions remain in force for generated pages.

---

[← Deployment and upgrades](deployment.md) · [Development and CI →](development.md)
