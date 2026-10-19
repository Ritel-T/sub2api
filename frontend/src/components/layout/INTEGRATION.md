# Console integration

## Pages and navigation

1. Register a route with translated metadata in `router/index.ts`.
2. Add its destination to `navigation/useConsoleNavSource.ts`. Apply role, run-mode and feature gates there before grouping or search.
3. Classify it in `navigation/consoleNavigation.ts`. Keep existing paths and meaningful query parameters so bookmarks and usage views retain their behaviour.
4. Render content inside `AppLayout`. Provide an `h1` when the page needs its own title placement; the shell otherwise uses route metadata.
5. Exercise the entry through section tabs, the drawer and search for each permitted role.

Do not add separate menu arrays in layout components. `mergeTabs` groups retain child destinations for search and route matching while presenting one section tab; the page owns child navigation.

`AppLayout` calls `provideConsoleNav()` once; descendants use `useConsoleNav()`. Workspace restoration stores only permitted structural route parameters, scoped by identity, role and run mode. A saved route must still be granted. Never store credentials or free-text filters there.

Feature gates control visibility, not server authorization. Register new public flags through `utils/featureFlags.ts` and preserve protected payment access checks.

## Responsive behaviour

- Preserve `inert`/`aria-hidden` on hidden navigation measurement elements.
- The drawer manages background scrolling, keyboard focus and focus restoration; dialogs above it retain their own focus handling.
- Section tabs determine shell height. `TablePageLayout` also measures its content offset and bottom padding; avoid fixed viewport deductions in individual list pages.
- Keep the skip link, translated control names, active-route states and minimum text sizes. Numeric badges use `sectionBadge`/`tabBadge` and `formatNavBadge`.

## Interactive tours

`AppLayout` passes `prepareTourTarget` and `finishTour` to `useOnboardingTour`. The active surface exclusively owns each real-link anchor through `tourId(path, surface)` and `tourAttr(path, surface)`.

Preparation enters the required section and opens the drawer when needed, including single-page sections. The tour controller owns advancement across route changes. Navigation closes the drawer without advancing the tour a second time. Submit steps advance only after the business operation succeeds.

The drawer owns the tour popover in the accessibility tree. Exit closes only a drawer opened by that tour, preserves an already-open drawer and leaves the route intact. Keep navigation-step descriptions short enough to expose the highlighted target on phones.

## Checks and preview

Use the installed frontend tools without changing dependencies:

```sh
node_modules/.bin/vitest run --maxWorkers=4 --minWorkers=2
node_modules/.bin/eslint . --ext .vue,.js,.jsx,.cjs,.mjs,.ts,.tsx,.cts,.mts
node_modules/.bin/vue-tsc --noEmit
node_modules/.bin/vite build --config vite.config.ts
```

For development, set `VITE_DEV_PROXY_TARGET` to the intended local backend and run Vite on loopback. An offline fixture establishes UI behaviour only. Do not send preview writes to production to fill fixture gaps.

Check desktop/phone widths in English and Hong Kong Traditional Chinese: workspace restoration, drawer click targets, search, language switching, balance states and tour navigation. Build and DOM-string checks cannot establish visual clickability.
