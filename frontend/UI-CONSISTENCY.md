# Shared interface contracts

Use the existing brand palette and font roles. Components that serve the same role share implementation and state behavior; density and business meaning may differ by role.

- Business lists use `useInfiniteList` and `InfiniteListSentinel`: append, preserve loaded data on failure, offer retry, invalidate obsolete filter requests, refresh the loaded range. Do not reintroduce manual previous/next business pages. Payload chunks, record-to-record navigation, time windows, comparison tables and editable matrices are different roles.
- `DataTable` and `KeyWorkspaceTable` preserve every field through primary columns and details. The column settings checkbox chooses the primary row, not permanent removal. Horizontal-only ancestor containers must explicitly set `overflow-y-hidden`; dialog lists use the real vertical dialog viewport.
- Filters use explicit `table-filter-workspace`, `filter-controls`, `open-filter-toolbar` or `workspace-filter-controls` scope. Search/native select/custom select/date triggers share the baseline. Ordinary editor fields retain their boundaries. Do not flatten every input with a global selector.
- Inputs and textareas forward validation and accessible attributes to the actual field, keep unique label/hint/error IDs, and retain wrapper layout classes.
- Switches expose a name, checked state and real disabled behavior. Use `Toggle`; the payment wrapper delegates to it. Standard and compact sizes are intentional density variants. CSS pointer-event suppression alone does not disable the keyboard.
- Modal dialogs use `BaseDialog`, confirmations use `ConfirmDialog`. Keep scoped focus/Tab/Escape/scroll handling and existing busy or mandatory-close restrictions. Nonmodal menus/calendar/column popovers are not modals.
- For imperative confirmation, use scoped `useConfirmation` and `ConfirmationPrompt`, rather than `window.confirm`. Cancel pending confirmations when a persistent editor closes or changes identity, and reject duplicate actions. Confirmation must still precede the original write.
- Content loading indicators expose a localized status name. Busy button spinners stay inside their already named/disabled button; do not add duplicate live announcements. An empty state is not loading.
- Keep actual platform/payment/error/quota semantics. Domain badge colors and dangerous versus primary buttons must not be collapsed into one generic style.

For broad changes, enumerate all runtime Vue files and route dependencies, including hand-written implementations and conditional branches. Keep a disposition for every exceptional pattern. Static coverage is not evidence that every role/data/language combination has been exercised in a browser.

2026-10-22 source audit and exact coverage: Windows workspace `reports/ritel-ui-consistency-20261022/`. See its coverage, controls, lists, dialogs and loading inventories, source hashes, test results and rendered proofs. That report records local changes separately from production deployment.

## Shared typography architecture

Authored product headings use `AppHeading`. Its `variant` selects a visual role; `as` independently selects the document heading level. A page title uses `variant="page"`, a settings section uses `section`, a nested section uses `subsection`, and a modal title uses `dialog`. Auth/marketing, minor, empty, metric and subtle roles are explicit. Do not infer presentation from h1/h2/h3, ancestor containers, or text color classes.

`public/brand/typography.css` owns the full family/size/weight/leading/tracking tuple and mobile variants. `brand.css` owns font assets/families. Tailwind families and all named size utilities reference those variables; direct classes and scoped `@apply` must compile to the same scale. Chart defaults read the root UI family. Layout classes may set spacing/color/alignment, not local heading typography.

`AppLayout` owns the route title by default. A page with its own page heading explicitly sets `:show-page-title="false"`; conditional loading/content ownership uses an explicit boolean expression. Do not inspect descendant DOM to guess title ownership.

Run `pnpm check:design`, `pnpm test:design`, and the AppHeading, Typography.css, AppLayout.title and BaseDialog tests. The AST guard runs before build and lint:check. It scans all runtime sources, rejects local heading overrides (including cross-component unscoped CSS), validates central Tailwind mappings, and checks exact, counted prose/screen-reader exceptions. Exceptions require a specific file/selector, count and reason; no page-wide wildcard.

2026-10-23 implementation and limits: Windows workspace `reports/ritel-type-architecture-20261023/`. Other products have a separate migration inventory; this frontend batch does not claim cross-product rollout or repair of the outstanding confirmation/control/list behavior findings.

## Confirmation and overlay lifecycle

Use `useConfirmation` with explicit local active/identity context for domain-owned prompts. `request({message, danger:false})` is for an ordinary decision; actions use `run({message,title,confirmText,danger}, async isCurrent => ...)` and keep the lock through the full mutation. Capture target IDs and payloads before awaiting. Check `isCurrent()` before dispatch after intermediate awaits, and before publishing results/errors or closing editors. Pass it into nested loaders too. A request already sent cannot be revoked by closing a dialog; invalidate its UI continuation and never replay unknown results.

Existing direct or custom confirmations use `useConfirmedAction` for the same execution contract, supply its `busy` to the confirmation, close pending decisions when their local target changes, and snapshot domain arguments. Lock cleanup is owned by the individual request generation, not whether its confirmation remains current; stale requests must release their own lock without releasing a newer request's lock. Failure retry is an explicit action after busy clears, never an automatic replay.

App provides a confirmation session context keyed by authenticated identity and route, without copying credential values. Shared prompt/ConfirmDialog invalidate on session and ancestor modal changes synchronously, including A→B→A. A component that creates its own BaseDialog must still declare its own `active: props.show`: a component cannot inject from its child.

Use the shared `overlayLifecycle` registry for modal focus/scroll ownership and interactive body portals. Register every interactive teleported popover with its actual trigger, root, open state and close callback. Escape dismisses the top owned popover before its modal; lower modal closure cannot steal top focus. Select Tab continues after its trigger; reverse Tab from search returns to the trigger. Pure display and layout teleports remain separate roles with a recorded disposition.

`check:design` runs typography, confirmation and control AST gates before build/lint; `test:design` checks the gates. The confirmation gate validates explicit intent, action/busy ownership, adapters and native-confirm escapes; it does not prove target dataflow. Keep runtime race, delayed-result, nested-loader, custom confirmation and browser keyboard regressions alongside the exact consumer/portal inventories. Do not treat a scan pass as a complete screen-reader, mobile-device or production transaction test.

2026-10-23 candidate implementation/evidence: Windows workspace `reports/ritel-confirmation-architecture-20261023/`. Preserve existing payment/identity/permission semantics. This does not complete the separate field, list, palette or cross-product adoption work.

## Development validation

Use [TESTING.md](TESTING.md) for scoped/watch checks, the deduplicated final verification command, escalation rules and test maintenance. The historical batch commands above describe coverage, not a requirement to rerun every gate after each edit.

## Field roles and loading

`workspace-controls.css` owns field geometry, focus, disabled and invalid states for native `.input`, Select and date controls. Brand variables own regular/compact heights and semantic error color. Edit fields retain a frame; explicit filter containers (or `data-field-role="filter"`) select the baseline. A field's `data-field-density="compact"` is an intentional variant. Keep class/style/role metadata on the wrapper; send labels, descriptions, constraints and events to the actual operable field. Clear actions are separate, named keyboard buttons.

Every Toggle/Select needs its business field name, not a generic enable/select placeholder. Select may use a real `label for`/id pair. `check-controls` rejects unnamed calls and local switch renderers so new pages must adopt the shared contract. Rendered behavior and valid name references still require consumer tests.

Use `LoadingRegion` once for a real skeleton group; repeated cells opt out of repeated live regions. Native table skeletons preserve table structure and expose one parent status. Health pulses, cursors and named busy-action icons keep their separate roles.

2026-10-24 integration and remaining list/cross-product work: Windows workspace `reports/ritel-ui-integration-20261024/README.md`. This candidate includes current QQ code, while unified-account integration stays separate.
