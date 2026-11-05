# Testing

Keep tests that can catch a distinct failure in user-visible behavior, data,
authorization, billing, or a protocol boundary. Prefer one representative test
per materially different behavior over an exhaustive matrix of equivalent inputs.
Do not test source spelling, component classes, private function structure, or
unchanged framework behavior. A failing test is not a reason to delete it; obsolete
expectations and coverage duplicated by another meaningful test are.

## Local changes

Run the existing tests for the changed behavior, plus lint/types when appropriate:

```sh
cd frontend
pnpm exec vitest run src/path/to/changed.spec.ts
pnpm run verify:local

cd ../backend
go test -tags=unit ./internal/package -run TestRelevantBehavior
```

Use integration tests when the change affects actual storage, transactions, or
cross-process behavior. PostgreSQL/Redis tests require a local Docker daemon and
create disposable fixtures. Never point tests at production.

## CI and release

- Frontend CI runs the retained frontend suite once. Backend CI runs
  `make -C backend test-all` once (`unit,integration` together). Path filters
  keep frontend-only changes from starting the backend suite. Untagged Go tests
  are not rerun in separate full unit and integration passes.
- Real multi-host and packaged re-login runtime checks have separate workflows
  because they exercise deployment boundaries that unit tests cannot. Their path
  filters limit unrelated runs. Dependency security checks and release packaging
  remain separate.
- `pnpm --dir frontend run build` performs type checking and compilation. It does
  not start test suites or custom source/style scanners. `check:i18n` remains an
  explicit focused check, also discovered by the retained frontend test suite.
- `pnpm --dir frontend run verify:release` is an explicit combined lint, test and
  build command. Do not repeat its tests after successful unchanged validation.
- Optional coverage reports are diagnostic; there is no arbitrary global target.

Inspect actual UI interactions when changing them. Reuse previous evidence while
its code and relevant conditions remain unchanged. Stop after the changed behavior
and integration points have sufficient evidence; do not rerun whole suites merely
for reassurance. Historical regression proofs remain recoverable in Git.
