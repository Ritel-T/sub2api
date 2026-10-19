# Console layout

`AppLayout` owns the authenticated console shell. Pages supply content through its default slot.

```vue
<template>
  <AppLayout>
    <h1>Page title</h1>
    <PageContent />
  </AppLayout>
</template>

<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
</script>
```

| Component | Responsibility |
| --- | --- |
| `AppLayout` | Shared navigation provider, skip link, content and tour lifecycle |
| `ConsoleTopBar` | Brand, workspace switch, navigation that fits available width, search and account utilities |
| `ConsoleSectionTabs` | Pages within the current section; reveals the active tab |
| `AppSidebar` | Navigation drawer, locale/documentation access, focus and scroll isolation |
| `ConsoleBottomNav` | First four visible sections and the drawer entry on phones |
| `ConsolePageTitle` | Route-title fallback when a page does not render its own `h1` |
| `TablePageLayout` | Actions, filters, locally scrolling table and pagination; natural height on phones |
| `FeatureSearch` | Permission-filtered search and keyboard navigation |
| `AuthLayout` | Public sign-in and account recovery layout |

Permissions and destinations live in `navigation/useConsoleNavSource.ts`; grouping, location matching and search hierarchy live in `navigation/consoleNavigation.ts`. Every navigation surface consumes that shared source. There is no separate desktop sidebar or collapsed-sidebar state.

The top bar switches to the drawer when space is limited. Opening the drawer makes background content inert; the backdrop stays below the drawer. Account menus preserve available/frozen balance semantics. Static branding comes from `utils/branding.ts`.

Shared console dimensions, badge/workspace styles and page padding are defined in `style.css`. Component styles describe their own surface. Corner, colour and type roles use the brand tokens.

See [integration notes](INTEGRATION.md) for route, permission, title and tour changes.
