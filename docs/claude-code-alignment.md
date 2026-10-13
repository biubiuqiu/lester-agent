# Claude Code interaction and runtime alignment

This implementation tracks the previously agreed comparison goals. Matching a behavior here is not a claim of complete Claude Code parity. No leaked source or copied proprietary implementation is included.

| Area | Current implementation | Remaining work |
| --- | --- | --- |
| Sending and stopping | Submission identity/digest, accepted-state recovery, attachment receipt reuse, model selection serialized and pinned to the run | Durable in-run input/queue semantics |
| File safety | Visible read receipts, SHA-256 checks, observed-range editing, native conditional writes | Actual before/after diff and limited conditional file rollback |
| Commands | Actual Bash/sh, explicit exit/timeout/cancel state, retained partial output; native and Docker integration validated | Live custom images require their own runtime validation |
| Background work and output | Private ownership handles, scoped task/log APIs, bounded raw logs, independent stop and UI, restart inspection without replay; atomic helper replacement preserves running supervisors | Live ACS deployments require their own provider validation |
| Providers | Multiple tool fragments, cumulative usage normalization, safe typed failures, real text-only readiness check | Live deployments require their own provider validation |
| Context | Complete-request budget estimates, actual/missing usage ledger, persistent scoped checkpoints, tool-free validated compaction with finite recovery/circuit breaker, literal protected input and tool groups, versioned Skill body deduplication, cached task-state restoration, visible FileState rebuilt each turn | Live model evaluations for summary quality and provider-specific context estimates |
| Collaboration | Drafts and recovery instructions preserved | Structured questions, executable Plan/approval gates and user adjustments |
| Project rules | Existing Agent instructions/resources remain scoped | Explicit project-rule source/version handling |
| Documentation and quality | Six-language UI/help maintained; PostgreSQL, native/Docker and desktop/mobile browser regressions | Repeat relevant checks and commit/push for each remaining batch |

MCP/hooks remain conditional extension work when a concrete integration needs them. This direction does not add automatic cross-conversation Memory/RAG, orchestration UI or a browser-management product.

Validation distinguishes local fixtures, real Docker commands and live cloud/provider integration. ACS fixture tests do not establish live ACS availability. Conditional file writes coordinate native helper writers; arbitrary Bash writes are outside that lock. Unknown launch receipts do not authorize a second command or a signal to an unverified process.

The first implementation batch passed the full backend suite with PostgreSQL fixtures, 57 frontend unit tests, six-language catalogue validation, lint and production build. All 108 browser scenarios passed across the full run and focused reruns after fixture corrections; a final 12-case desktop/mobile check covers model readiness and managed tasks. The rebuilt sandbox passed its offline non-root runtime check and real Docker filesystem, terminal and managed-task tests. No live model, ACS, OAuth or SMTP integration is claimed by these checks.

The context checkpoint batch passed the full backend suite with PostgreSQL fixtures for durable boundaries, restart/model-change restoration, usage request numbering, scope, malformed/tool-calling/ineffective summaries, cancellation, bounded context-error recovery and removed-file-read permissions. Frontend unit tests, lint, build and all six catalogues (1,407 source messages) passed. All 112 browser scenarios passed across the complete run and focused reruns after fixing an overly broad test locator; the final 22-case run covers checkpoint states and all-language public help. Migration 020 preserves original messages; summary-quality evaluation against a live model remains a deployment-level check.
