# Issue #582 — Redis 6.0 compatibility

- Scope: isolated fix/issue-582-redis-compatibility branch, baseline production `08d69a522`; sole writer. Do not touch #578 service usage snapshot chain/workflow.
- Read DEV_GUIDE.md and the complete `gh issue view 582 --json body,comments` (no comments). No applicable AGENTS.md found in worktree/ancestor directories.
- Confirmed scheduler metadata uses SET GET (Redis >=6.2), freezing publication/outbox on 6.0. Official Redis command histories also confirm GETDEL/ZRANGE options >=6.2 and EXPIRE NX >=7.0.
- Chosen approach: preserve Redis 6.0 support with atomic GETSET, Lua session take/fixed-window count, and legacy sorted-set commands. Keep scheduler fencing, revision invalidation, last-used side keys, grace TTLs, and error handling.
- Additional audit hits: user message/concurrency cleanup, shared OAuth/SIWC and passkey take, IP risk/serverless sorted-set reads, API key fixed-window EXPIRE NX.
- Regression-first: add actual Redis integration behavior tests before implementation; CI-only baseline RED and exact-head GREEN on Redis 6.0/7.x. Existing tests/skips must not be weakened.
- Local policy: no Go compilation/tests, Docker, frontend suite, production SSH/DB/Redis or service changes. Only static checks/gofmt/offline scripts locally. Dynamic validation pending remote CI.
- Delivery pending: commit/push, Draft PR to production (Fixes #582), inspect all push/PR raw failures, freeze source for parent review. No merge/release/deploy.
