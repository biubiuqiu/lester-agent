# Lester security audit — 2026-10-10

Baseline: `c07fcc7` on `main`. This review covers tracked source, dependency versions, full available Git history, authentication/authorization, private files and HTML previews, provider HTTP requests, terminal transports and deployment templates. Tests use disposable local fixtures, not production accounts or credentials. Severity below reflects Lester's exposure; an advisory match alone does not demonstrate exploitation.

## Fixed in this change

| Priority | Finding and trigger | Resolution and evidence |
| --- | --- | --- |
| High | Model provider redirects could forward credentials to another host; HTTP 307/308 also replayed private conversation bodies. Go does not strip custom `x-api-key`, Azure `api-key` or AWS session headers like it does some standard credentials. Requires a redirecting/misconfigured/compromised configured endpoint. | OpenAI/Anthropic/Azure and Bedrock requests reject redirects, including same-host redirects. Injected HTTP clients retain their transport/timeouts without being mutated. A two-server regression failed before the fix; 301/302/303/307/308 now leave the redirect target untouched. Bedrock has a signed-request regression as well. |
| Medium | `middleware.RealIP` accepted arbitrary forwarding headers; `True-Client-IP` could pass through the supplied gateway and change IP rate buckets. Identity buckets still limited attempts against an individual account. | Only explicitly trusted TCP peers can provide `X-Forwarded-For`; walk its chain from the right. Ignore `True-Client-IP` and `X-Real-IP`, preserve the actual peer, and strip `True-Client-IP` at the gateway. Tests cover direct/untrusted requests, forged chain prefixes, malformed headers, IPv6 and mapped IPv4. |
| Medium | Terminal WebSocket input and API relay messages had no read-size limit. An authenticated client could force large allocations on shared services. | Set a 1 MiB frame limit at Sandbox Service and both sides of the API relay. A real WebSocket fixture accepts normal input and rejects oversized input with close code 1009 before it reaches the shell; this test failed before the fix. |
| Medium | Redis errors silently bypassed authentication/upload rate limits. Refresh rotations also had no account/IP rate limit and could produce excessive token rows. | A configured but unavailable Redis now denies rate-limited operations. Refresh is limited to 60 attempts per minute by account and IP after resolving a known credential; rejected refreshes leave the token family intact. A failing Redis dialer verifies limits cannot be bypassed. Production always configures Redis; nil Redis is retained only for isolated service/test construction. |
| Medium | Existing terminal and SSE connections authenticated only at entry and could survive session revocation/account disabling/workspace removal. | Authenticated requests recheck the stable session family, account eligibility and workspace membership every 30 seconds, with a five-second database deadline. Cancellation closes terminal relays and event streams. PostgreSQL tests verify logout, disabling and membership removal cancel the connection context while rolling refresh preserves it. Revocation is bounded, not instantaneous. |
| High / critical advisory matches | Frontend production dependencies included Next.js RCE/SSRF/cache issues and vulnerable image/source-map dependencies. Go dependencies and the old Go 1.25.1 toolchain matched HTTP, HTML, Unicode and database-driver advisories. | Upgrade the compatible Next.js patch line and affected transitive dependencies; upgrade Go toolchain/build images/CI and affected modules. Add production `pnpm audit` and symbol-level `govulncheck` to CI. Exact versions and conditional exposure are below. |

The updated Next ESLint rules also caught a ref read while rendering OAuth links. The login page now renders the validated return URL from state and continues to read its original URL in event handlers; login mode switches and preview return navigation are covered by browser checks.

## Dependency scan

Frontend versions: Next.js and eslint-config-next **16.3.4 → 16.3.8**, sharp **0.35.4 → 0.35.5**, source-map-js **1.2.1 → 1.2.2**, brace-expansion **1.1.18 → 1.1.21** / **5.0.9 → 5.0.12**. The lockfile records the resolved versions.

- Before: `pnpm audit` reported **16 advisory entries** (1 critical, 8 high, 6 moderate, 1 low). Several entries affect the same dependency; this is not a count of independently exploitable Lester bugs.
- After: production audit reports **0 known advisories**. Full audit retains **1 high advisory in a development dependency**, described below.
- The critical Next.js advisory is [GHSA-vcvr-r3jv-pc5j](https://github.com/advisories/GHSA-vcvr-r3jv-pc5j), concerning `next/og` ImageResponse. Lester currently does not use `next/og`/ImageResponse, so reachable RCE was not demonstrated. The framework is patched anyway.
- Other patched Next.js advisories include image optimization SSRF, cache poisoning, Draft Mode/cache isolation, metadata routing and development-server disclosure. This review does not claim every affected feature is enabled in Lester.

Go: **1.25.1 → 1.26.9**; chi **5.2.3 → 5.3.0**; pgx **5.7.6 → 5.9.2**; klauspost/compress **1.18.0 → 1.18.7**; x/crypto **0.44.0 → 0.57.0**; x/net **0.47.0 → 0.60.0**; x/text **0.31.0 → 0.42.0**; related x/sys/x/sync/x/term modules advance to required compatible versions. The selected security fixes require Go 1.26; all four build Dockerfiles and CI use 1.26.9 consistently.

- Before: **94 distinct Go advisories**, of which **54** had symbol-level call paths. Most concern the old standard library or packages sharing a module. Call reachability is conservative static evidence, not proof that attacker-controlled inputs reach every vulnerable condition.
- After: **0 advisories with called vulnerable symbols**; one unused-module advisory remains. `govulncheck` exits successfully at the symbol level.
- Relevant call paths included HTML parsing during publication, HTTP clients/servers, Unicode processing, chi RealIP and pgx sanitization. Parameterized application queries remain in place; no application SQL-injection exploit was demonstrated.

The managed network blocks `vuln.go.dev`. For this run the Go database was generated using the official `golang/vulndb` repository, including its history, at snapshot **`f5aaa67f6ede4dff5b467dd6beba251f3c706b05`**. This preserves the official database protocol and data; it is not a substituted third-party feed.

## Remaining issues and deployment decisions

| Priority | Status | Next action |
| --- | --- | --- |
| High for a public service accepting untrusted accounts | Users can configure model endpoints, and provider requests originate from the API network. Private/self-hosted endpoints are an intentional supported feature; stopping redirects does not prevent a direct request to an internal address. This scan did not access cloud metadata or production internal services. | Before public multi-user hosting, enforce an operator-owned endpoint allowlist or API egress controls covering loopback, metadata/link-local and unrelated private networks, while explicitly allowing required model services, database, Redis, object storage, Sandbox Service, OAuth and SMTP. Separate that policy from sandbox network isolation. Avoid a blanket private-IP ban that breaks authorized local models. |
| High advisory, development only | [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm): `eslint-config-next → @next/eslint-plugin-next → fast-glob → micromatch → braces@3.0.3`, stack exhaustion on crafted patterns. Registry metadata publishes no patched version. It is absent from production dependency audit. | Track the upstream fix and then update the lockfile. Treat untrusted lint patterns and build inputs as potentially hostile. The advisory is not ignored or represented as fixed. |
| Unused-module advisory | `GO-2026-5932`: x/crypto's unmaintained `openpgp` package has no fix. Lester needs x/crypto for Argon2 but does not import/call openpgp. | Keep it unused; never introduce openpgp through this module. Track module-level scan results separately from symbol-level failures. |
| Conditional deployment risk | Two historical gitleaks records refer to the same old `.env.example` master key, a recognizable deterministic fixture. Current `.env.example` leaves it blank and requires generation. No additional credential leak was confirmed by this scan; a diff scan also matched an existing public Go module checksum, which is not a credential. | If an old deployment copied that example key, migrate/re-encrypt stored model credentials under a new random key and rotate affected provider credentials. Do not simply replace the key and make existing encrypted credentials unreadable. This review did not inspect or rotate deployment secrets. |
| Capacity hardening | Frame limits and refresh rate limits do not impose a total per-account quota on terminal connections, concurrent expensive requests, stored token history or user data. | For a public hosted service, add connection/concurrency/storage quotas, token-history maintenance and edge admission limits. Monitor shared-service resource usage. |

### Upgrade/configuration notes

This change adds no database migration. Rebuild API, Web, Sandbox Service and Artifact Host to deploy the fixes; a source pull alone does not replace running containers. Existing Computer images need rebuilding if the deployed image contains the Go toolbox and is to receive the updated build toolchain.

`AUTH_TRUSTED_PROXY_CIDRS` defaults to empty and ignores all forwarding headers. Behind one gateway, all users then share its IP bucket. Configure only the actual gateway/Ingress peer addresses or controlled subnet, using comma-separated CIDRs; Helm uses `config.auth.trustedProxyCIDRs`. Inspect the deployment's actual network rather than copying an unrelated example range. `/0` is rejected. Restrict API reachability to the trusted proxy and update the setting if its address changes. Both README translations and public help describe this requirement.

Redis must be healthy for registration/login/mail/avatar/refresh operations subject to limits. A temporary Redis failure rejects those operations until recovery; existing valid access credentials still authenticate normally.

## Static and secret review

`gosec` initially reported **54 findings across 77 Go files / 12,502 lines**. Its severity labels were manually evaluated against the actual trust boundaries:

- Docker `exec.CommandContext` uses argument arrays and validated provider identifiers; the user's requested shell is deliberately executed inside the user's resource-limited Computer. This is distinct from shell injection into the API host.
- Toolbox filesystem paths have lexical/resolved boundary checks and bounded atomic operations; variable-path alerts alone did not establish an escape. This does not prove immunity to every concurrent filesystem race within a user's own Computer.
- Upload parsers are already preceded by `http.MaxBytesReader` and bounded file reads; generic form-parser alerts did not establish unbounded uploads.
- Private raw files are octet-stream attachments with `nosniff`/`no-store`; HTML is served with CSP sandbox isolation and the UI iframe has only `allow-scripts`. Public executable artifacts use a separate hostname. Generic response-body XSS alerts did not demonstrate application-origin execution.
- Cookie security is assigned dynamically, with HttpOnly/SameSite and HTTPS/Secure validation. OAuth client secrets come from server configuration; detected struct assignments are not hardcoded credentials.
- Password-hash decoding checks exact sizes before conversion; fixed artifact redirect paths do not accept foreign origins. Operator-controlled numeric configuration and unchecked best-effort cleanup calls remain maintenance concerns rather than confirmed public exploits.

The final SAST rerun retains the same 54 reviewed alert categories. The scanner findings were not suppressed simply to achieve a zero-alert SAST result. Gitleaks inspected available Git history with full redaction; raw scan outputs and local fixtures remain outside the repository.

## Validation and reproducibility

Validated: full `go test ./...` with real disposable PostgreSQL; focused `-race` for auth, sandbox, model integration and HTTP helpers; 38 frontend tests; frontend lint and production build; Compose configuration; Helm lint/render with a trusted-proxy value; Go 1.26.9 Alpine image manifest availability; gateway's five fixture groups (cookies/headers, private HTML, 25 MiB uploads, SSE, WebSocket), including forged `True-Client-IP` stripping.

Browser checks use Playwright/Chromium because the Browser plugin is unavailable: production Next.js at `http://localhost:13007`, desktop 1440×900 and mobile 390×900, with intercepted fixture API responses. Homepage/help navigation, proxy documentation, Google OAuth return URL, login/register mode changes, login return to standalone HTML, opaque iframe isolation and local script interaction passed, with no page errors or horizontal overflow. Actual credentials/refresh/revocation logic is verified separately against PostgreSQL; browser fixtures do not test live Google/GitHub consent or delivery through an actual SMTP provider.

```bash
cd frontend
pnpm audit --prod --audit-level=high
pnpm audit  # currently reports the unpatched development-only braces advisory
pnpm test && pnpm lint && pnpm build

cd ../backend
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
go test ./...
go test -race ./internal/auth ./internal/sandbox ./internal/model/integration ./internal/httpapi
```

For a managed network with the same restriction, clone the official `golang/vulndb` repository with history, run its `cmd/gendb`, and pass `govulncheck -db file:///absolute/path/to/database ./...`. Do not disable TLS checks or bypass network policy.

Tools used: pnpm 10.17.1 audit; govulncheck 1.8.0; gosec 2.29.0; gitleaks 8.30.1; manual source review and local regression fixtures. This is a scoped source/dependency review, not a production penetration test or a container/OS vulnerability scan. No zero-vulnerability guarantee is implied.
