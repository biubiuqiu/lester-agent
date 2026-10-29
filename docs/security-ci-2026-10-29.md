# CI dependency investigation — 2026-10-29

Recent aggregate CI failures were not all frontend regressions. The public run summaries distinguish the failing jobs:

| Run | Evidence |
| --- | --- |
| [113](https://github.com/biubiuqiu/lester-agent/actions/runs/40515208612) | Browser regression: 394 passed. Go security, frontend security and native builds failed. |
| [114](https://github.com/biubiuqiu/lester-agent/actions/runs/40661800693) | Browser regression: 440 passed. Go security, frontend security and native builds failed. |
| [115](https://github.com/biubiuqiu/lester-agent/actions/runs/40711706568) | Browser regression: 450 passed. Go dependency security failed; native checks were intentionally skipped by path selection. |
| [116](https://github.com/biubiuqiu/lester-agent/actions/runs/40806012065) | Go security annotations identify SMTP certificate verification and HTML parsing/rendering call paths. |

## Confirmed dependency fixes

The existing Go scan was reproduced locally with the official Go vulnerability repository snapshot `8f7e390a0ff7345d74ecd7a056a1073214466db9`. The environment can read public GitHub pages/repositories but its network policy denies `api.github.com` and the live vulnerability database. The local scan uses the complete official repository's `data/osv` files through govulncheck's file database support; CI retains the normal live database and the same reachable-symbol failure policy.

| Finding | Affected installation | Fix |
| --- | --- | --- |
| [GO-2026-6809](https://github.com/golang/vulndb/blob/master/data/reports/GO-2026-6809.yaml) / CVE-2026-111943 | Go 1.26.9, reachable through SMTP TLS certificate verification | Go 1.26.10 in `go.mod` and all four Go Docker build stages |
| [GO-2026-6860](https://github.com/golang/vulndb/blob/master/data/reports/GO-2026-6860.yaml) / CVE-2026-78661 | `golang.org/x/net/html` parsing before 0.62.0 | `golang.org/x/net` 0.62.0 and its required compatible modules |
| [GO-2026-6866](https://github.com/golang/vulndb/blob/master/data/reports/GO-2026-6866.yaml) / CVE-2026-97045 | `golang.org/x/net/html` rendering before 0.62.0 | `golang.org/x/net` 0.62.0 |
| [GHSA-2464-fhr4-w9xw](https://github.com/advisories/GHSA-2464-fhr4-w9xw), [GHSA-p3m5-59mp-jv5r](https://github.com/advisories/GHSA-p3m5-59mp-jv5r), [GHSA-673q-mjm6-r5xr](https://github.com/advisories/GHSA-673q-mjm6-r5xr) | Next.js 16.3.8; two critical and one high production advisories | Next.js and matching ESLint configuration 16.4.1, with the frozen lockfile updated |

The updated local Go scan reports **zero reachable vulnerabilities**. It still reports one advisory in a required module whose affected package/symbol is not imported or called; this is not a claim that every transitive module is advisory-free. The production pnpm audit reports zero advisories at all severities. Security checks remain enabled and fail on scan errors as well as blocking findings.

Local validation includes stable repeated `go mod tidy`, gofmt/vet, uncached Go tests against disposable PostgreSQL and executable builds; frontend tests, lint and production build; 38 desktop/mobile smoke tests; Android JVM/lint/Debug/Release builds; Compose/gateway validation; and 15 CI selection/error-reporting tests plus actionlint. Full browser regression and the next hosted native build are verified separately rather than inferred from these smoke checks.

## Native build diagnosis

Older iOS and Android jobs failed with exit codes 65 and 1 respectively. Public summaries did not expose the actual compiler/SDK failure, and complete step logs require authenticated Actions access. Those exit codes alone do not establish a source-code cause. The same Android resource, JVM, lint, Debug and Release commands pass locally.

CI now captures SDK, compiler, Core tests and Gradle output, publishes bounded actionable annotations and retains failure logs for seven days. The original commands retain Bash `pipefail`, so log reporting cannot hide a failed build. A Linux Swift parse or Core test is never reported as an iOS SDK build.

The next [hosted run, 117](https://github.com/biubiuqiu/lester-agent/actions/runs/40840561491), confirmed both security jobs passed and exposed the native causes:

- **iOS:** Xcode reported an incorrect WebKit argument label in `WorkspaceWebView.swift:30`. `callAsyncJavaScript` uses `in:in:` for the frame and content world, rather than `in:contentWorld:`. The correction retains the trusted current-page guard and `.page` world.
- **Android:** refreshing dependency metadata reproduced the CI failure locally. Three Maven metadata artifacts had no SHA-256 entry, despite warm-cache builds succeeding. Their bytes were independently downloaded from canonical Maven Central and matched against the Gradle cache before adding the missing entries. Dependency verification remains strict; no dependency version or locking rule was relaxed.

| Maven metadata artifact | Verified SHA-256 |
| --- | --- |
| `com.google.guava:guava-parent:33.3.1-jre` POM | `55441db27e8869dfefe053059bdf478bdc7e95585642bf391f0023345fd56287` |
| `org.junit:junit-bom:5.10.2` module metadata | `de23b114b3e4119a8fe6eb17bed5a3852816698bace67071579d6d927ebb080a` |
| `org.jetbrains.kotlinx:kotlinx-coroutines-bom:1.8.0` POM | `1239e9dbe1397cd5971342956b2511bc3ace7b641842e4372a088dcfa8b9ad55` |

After adding these entries, Android's full native command succeeds with `--refresh-dependencies` and strict verification. Swift source parsing and the six Core tests pass locally; the hosted Xcode build is the authority for the corrected WebKit call.

No migration, user data change, provider credential, deployed service or application permission change is required. Rebuild the affected application/runtime images to receive the dependency and toolchain patches; existing Computers and volumes are preserved.
