# App connections

[Documentation](README.md) · [简体中文](zh-CN/app-connections.md)

Connect GitHub, Gmail, X, Feishu and Slack from **Settings → App connections** or the account menu. Connect opens the provider’s authorization page in the same browser tab and returns to Lester. The original task/project return link and account-scoped drafts are preserved. Each user has one account per provider in each workspace; another workspace member cannot use that grant.

Connections authorize task Agents to **search and read**. They do not send mail/messages, post, mark mail as read, download attachments, follow source links or automatically import a mailbox. The independent personal assistant remains memory/delegation-only. Connecting a work app neither signs in to Lester nor links a Lester sign-in identity.

## Deployment

Apply every missing migration through `backend/migrations/000032_app_connections.up.sql` in numeric order after a backup and stopping API writes/active tasks. Rebuild API and Web together. Fresh Compose initialization mounts 001–032; existing volumes require explicit migration. Preserve the existing `MASTER_KEY_BASE64` and volumes. Migration 032 rollback removes app grants and pending OAuth flows only, not accounts, sessions, task files or completed external effects.

Create provider applications with the following optional credential pairs. Only API receives the secrets; never put them in `NEXT_PUBLIC_*`, a Skill or a Computer. These variables are independent of `GOOGLE_OAUTH_*` / `GITHUB_OAUTH_*` used for Lester sign-in.

| Provider ID | Environment variables | Exact callback (append to `WEB_ORIGIN`) |
| --- | --- | --- |
| `github` | `CONNECTOR_GITHUB_CLIENT_ID`, `CONNECTOR_GITHUB_CLIENT_SECRET` | `/api/v1/connectors/github/callback` |
| `gmail` | `CONNECTOR_GMAIL_CLIENT_ID`, `CONNECTOR_GMAIL_CLIENT_SECRET` | `/api/v1/connectors/gmail/callback` |
| `x` | `CONNECTOR_X_CLIENT_ID`, `CONNECTOR_X_CLIENT_SECRET` | `/api/v1/connectors/x/callback` |
| `feishu` | `CONNECTOR_FEISHU_CLIENT_ID`, `CONNECTOR_FEISHU_CLIENT_SECRET` | `/api/v1/connectors/feishu/callback` |
| `slack` | `CONNECTOR_SLACK_CLIENT_ID`, `CONNECTOR_SLACK_CLIENT_SECRET` | `/api/v1/connectors/slack/callback` |

Use the public same-origin gateway/Ingress HTTPS address for `WEB_ORIGIN`, with `SESSION_COOKIE_SECURE=true`. Only localhost HTTP development is allowed. Register exact callback URLs without wildcard redirects. Empty/incomplete credential pairs leave Connect unavailable; metadata is not a live provider connection test. Native iOS/Android shells require authorizing in the browser version first because system browsers and WebViews do not share the OAuth browser cookie. Connected task grants then work in the app.

Provider consoles: [GitHub](https://github.com/settings/apps), [Google Cloud](https://console.cloud.google.com/apis/credentials), [X](https://developer.x.com/en/portal/dashboard), [Feishu](https://open.feishu.cn/app), [Slack](https://api.slack.com/apps).

### Provider setup and scope

- **GitHub:** For private repositories, create/install a **GitHub App**, enable user authorization, and grant repository **Metadata, Contents, Issues and Pull requests: read-only**. Use its OAuth client ID/secret, not the App ID/private key. Repository access is the intersection of user access and installations. No `repo` OAuth scope is requested: a classic OAuth App therefore supports public resources only. Code search and some resources have additional provider restrictions. Authorizing does not install a GitHub App; finish installation separately. Expiring user tokens can refresh when the provider returns a refresh token.
- **Gmail:** Create a Google OAuth **Web application**, enable Gmail API and the consent screen/test users. Requested scopes: `openid email https://www.googleapis.com/auth/gmail.readonly`; offline consent requests refresh access. Production/public distribution may require Google verification and additional restricted-scope security review. Google testing grants may expire early. Reconnect if no usable refresh grant remains. The immutable Google subject identifies the connected account; this does not merge Lester accounts.
- **X:** Create an OAuth 2.0 confidential Web App with user authentication. Scopes: `tweet.read users.read offline.access`. Recent search and tweet lookup require an eligible API plan; a successful OAuth flow does not guarantee search access. No write scopes are requested.
- **Feishu (China endpoint):** Create an application with user OAuth v2, register the callback, enable `offline_access contact:user.base:readonly im:chat:readonly im:message:readonly docx:document:readonly`, publish or allow test users as appropriate. Tenant administrator approval and document/chat visibility still apply. The adapter searches **chat names**, reads visible chat/messages and reads a known authorized **Docx document ID**. It does not claim global message/document full-text search, old Docs, Sheets or international Lark endpoint support.
- **Slack:** Create an OAuth v2 Slack app. Add **User Token Scopes** `search:read channels:history groups:history im:history mpim:history` (not bot scopes). Install/authorize in a workspace where permitted. The adapter uses the authorized user token returned in `authed_user`, including rotation when supplied. Workspace policies, app approval and Slack rate limits apply; thread visibility remains limited to that user. Bot tokens are rejected.

A provider may require administration, consent review or a paid plan. The implementation is validated against protocol fixtures and real PostgreSQL; no live account authorization is claimed without deployment credentials.

## Agent usage and bounded data

The server exposes two registered task tools, available during read-only Plan:

1. `list_app_connections {}` returns safe availability/account metadata for the **accepted run submitter**.
2. `read_app` accepts `provider`, `action: search|read`, optional `kind`, `query`, `resource`, `cursor`, `limit` (1–20; X search minimum 10). It never accepts a token, API host, user or workspace ID. Use the returned `next_cursor` for another explicit page.

| Provider | Search | Read `resource` |
| --- | --- | --- |
| GitHub | `kind: issues` (default), `repositories`, `code`; provider query syntax | `owner/repo`, `owner/repo/issues/N`, `owner/repo/pulls/N`, `owner/repo/contents/path` |
| Gmail | `kind: messages`; Gmail query syntax | Returned message ID |
| X | `kind: tweets`; recent-search syntax | Returned tweet ID |
| Feishu | `kind: chats`; chat-name query | `chat:CHAT_ID`, `message:MESSAGE_ID`, `doc:DOCUMENT_ID` |
| Slack | `kind: messages`; Slack query syntax | `CHANNEL_ID:message_timestamp` (thread) |

Example task: “Search my connected Gmail for `from:vendor@example.com newer_than:7d`, read the relevant messages, and summarize the delivery dates with source IDs.” Only an explicit task/model call reads the app. Files subsequently written by the Agent use existing task permissions.

Results are normalized external reference data, not trusted instructions. Responses are capped at 2 MiB before parsing; normalized results have at most 20 entries and 12,000 text characters, with truncation/pagination indicators. Gmail search retrieves bounded metadata; reading gets plain text (or stripped HTML) without external images/attachments and never changes unread status. Large, malformed or inaccessible responses produce explicit errors, not a fake empty result. Source access does not verify whole documents or grant publication rights.

GitHub search reports `truncated: true` when the provider marks results incomplete or when the query exceeds its 1,000-result window. Use `next_cursor` for available pages and narrow the query when the window is exhausted; an empty cursor does not prove the full search was read. Provider rate limits are reported separately from missing permission. Slack search results that point to replies retain the parent thread reference for a subsequent thread read.

## Security, refresh and disconnect

Browser mutations use existing authentication and strict same-origin validation. Ten-minute single-use hashed state, an HttpOnly provider cookie, the original live session family, current browser credential and connection generation bind each flow. PKCE S256 is used except for Slack’s confidential OAuth v2 flow. Provider endpoints are fixed server-side; redirects are rejected and raw provider errors/credentials are never sent to the UI or model.

Access/refresh grants and PKCE verifiers are encrypted with AES-GCM using the existing master key and connection/actor-associated data. Task reads resolve `runs.submitted_by` and check an active run, account and membership, independently of who originally created the conversation. No grants enter a Computer, model prompt, browser storage or application URLs. Resource text itself is sent to the task’s chosen model, just like other requested reference material.

Expired credentials refresh serially per connection. A successful token rotation commits **before** resource reads so a later read failure cannot cause reuse of the consumed refresh token. An uncertain refresh response requires reconnecting instead of automatic replay. Revoked access shows “Reauthorization required”; resource-read permission/plan/rate errors preserve the grant. Disconnect uses revision checks, deletes local encrypted credentials and prevents old callbacks restoring them. It does **not** revoke the provider-side application installation/consent: revoke that in the provider’s settings when required. Reads already completed and task transcripts remain.

## Verification

Run backend checks with `LESTER_TEST_DATABASE_URL` pointed at a disposable PostgreSQL database. Connector tests apply all migrations and cover five provider protocols, read-only GETs, encrypted storage, user/run isolation, OAuth expiry/replay/denial, stale callbacks after disconnect/logout/disable/new flow, refresh concurrency/uncertainty, origin checks and rollback. Frontend unit/browser fixtures cover authorization URL validation, connection states, canceled authorization, disconnect receipt recovery, six languages, responsive/keyboard behavior and native-browser guidance. A scripted-model integration test also runs the actual task executor through list → search → read in ordinary and read-only Plan modes, checking matching tool receipts, preserved source text, no execution approvals and no Computer API requests. These fixtures do not verify real OAuth app registration, provider review or API subscriptions.

Refresh attempts record durable intent before contacting the provider. A crash or cancellation after token consumption requires reconnecting instead of replaying the previous refresh token. A per-connection PostgreSQL advisory guard serializes readers across API replicas.
