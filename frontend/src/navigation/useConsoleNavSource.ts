import { getCustomMenuLabel } from '@/utils/customMenu'
import { centralAccountEnabled } from '@/utils/accountConfig'
import { computed, inject, nextTick, onMounted, onUnmounted, provide, ref, watch, type ComputedRef, type InjectionKey } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAdminSettingsStore, useAppStore, useAuthStore, useSupportTicketStore } from '@/stores'
import { FeatureFlags, makeSidebarFlag } from '@/utils/featureFlags'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { useMerchantTestPaymentAccess } from '@/composables/useMerchantTestPaymentAccess'
import {
  arrangeConsoleSections, consoleNavigationText, consoleSearchTree, legalWorkspaceDestination, resolveConsoleLocation,
  workspaceLocation, type ConsoleNavigationText, type ConsoleNavItem, type ConsoleSection, type ConsoleTab, type ConsoleWorkspace,
} from './consoleNavigation'
import { consoleRouteTitle } from '@/utils/consoleShell'
import { resolveRouteMetaKeys } from '@/router/title'
import {
  RequestCaptureIcon, OpsMonitoringIcon, SmartOpsIcon, QualityOpsIcon, AccountOpsIcon,
  TokenGuardIcon, CredentialOpsIcon, PelicanTestsIcon, SupportTicketIcon, SupportTicketInboxIcon,
  DashboardIcon, KeyIcon, BatchImageIcon, ChartIcon, GiftIcon, UserIcon, UsersIcon, FolderIcon,
  ChannelIcon, CreditCardIcon, RechargeSubscriptionIcon, GlobeIcon, FlowIcon, ServerIcon,
  PluginIcon, BellIcon, TicketIcon, CogIcon, OrderIcon, OrderListIcon, SignalIcon, GalleryIcon,
  ShieldIcon, PriceTagIcon,
} from './consoleNavIcons'

interface NavItem extends ConsoleNavItem {
  children?: NavItem[]
  hideInSimpleMode?: boolean
  /** Only an explicit false hides an item while settings load. */
  featureFlag?: () => boolean | undefined
}

function applyFeatureFlags(items: NavItem[]): NavItem[] {
  const out: NavItem[] = []
  for (const item of items) {
    if (item.featureFlag && item.featureFlag() === false) continue
    if (item.children) {
      out.push({ ...item, children: applyFeatureFlags(item.children) })
    } else {
      out.push(item)
    }
  }
  return out
}

export interface ConsoleNavSource {
  navText: ComputedRef<ConsoleNavigationText>
  userNavItems: ComputedRef<NavItem[]>
  adminNavItems: ComputedRef<NavItem[]>
  workspace: ComputedRef<ConsoleWorkspace>
  workspaceOptions: ComputedRef<{ value: string; label: string }[]>
  sections: ComputedRef<ConsoleSection[]>
  activeLocation: ComputedRef<{ section: ConsoleSection; tab: ConsoleTab } | undefined>
  searchItems: ComputedRef<ConsoleNavItem[]>
  homePath: ComputedRef<string>
  pageTitle: ComputedRef<string>
  switchWorkspace: (value: string) => void
  handleNavigate: (path: string) => void
  tourId: (path: string, surface?: 'tabs' | 'drawer') => string | undefined
  tourAttr: (path: string, surface?: 'tabs' | 'drawer') => string | undefined
  prepareTourTarget: (selector: string) => Promise<boolean>
  finishTour: () => void
}

const ConsoleNavSourceKey: InjectionKey<ConsoleNavSource> = Symbol('consoleNavSource')

export function provideConsoleNav() {
  const { t, locale } = useI18n()
  const route = useRoute()
  const router = useRouter()
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const adminSettingsStore = useAdminSettingsStore()
  const supportTicketStore = useSupportTicketStore()
  const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()
  const { merchantTestPaymentAccess } = useMerchantTestPaymentAccess()

  const isAdmin = computed(() => authStore.isAdmin)
  const navText = computed(() => consoleNavigationText(locale?.value ?? 'en'))

  const flagChannelMonitor = makeSidebarFlag(FeatureFlags.channelMonitor)
  const flagPublicPayment = makeSidebarFlag(FeatureFlags.payment)
  const flagPayment = () => flagPublicPayment() || merchantTestPaymentAccess.value
  const flagAvailableChannels = makeSidebarFlag(FeatureFlags.availableChannels)
  const flagPelicanShowcase = makeSidebarFlag(FeatureFlags.pelicanShowcase)
  const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)
  const flagModelPlaza = makeSidebarFlag(FeatureFlags.modelPlaza)

  const purchaseNavLabel = computed(() => {
    switch (resolveSiteBillingMode(appStore.cachedPublicSettings)) {
      case 'recharge_only':
        return t('nav.recharge')
      case 'subscription_only':
        return t('nav.subscribe')
      default:
        return t('nav.buySubscription')
    }
  })
  const flagAffiliate = makeSidebarFlag(FeatureFlags.affiliate)
  const flagRiskControl = makeSidebarFlag(FeatureFlags.riskControl)
  const flagPluginManagement = makeSidebarFlag(FeatureFlags.pluginManagement)
  const flagSupportTickets = makeSidebarFlag(FeatureFlags.supportTickets)
  const flagUserSupportTickets = () => flagSupportTickets() && !authStore.isAdmin
  const flagOpsMonitoring = () => adminSettingsStore.opsMonitoringEnabled
  const flagAdminPayment = () => appStore.cachedPublicSettings?.payment_enabled === false
    ? merchantTestPaymentAccess.value
    : adminSettingsStore.paymentEnabled || merchantTestPaymentAccess.value
  const flagBatchImageAccess = () => canUseBatchImage.value

  function buildSelfNavItems(): NavItem[] {
    return [
      { path: '/dashboard', label: t('nav.dashboard'), icon: DashboardIcon },
      { path: '/keys', label: t('nav.apiKeys'), icon: KeyIcon },
      { path: '/batch-image', label: t('nav.batchImage'), icon: BatchImageIcon, hideInSimpleMode: true, featureFlag: flagBatchImageAccess },
      { path: '/model-plaza?embedded=1', label: navText.value.models, icon: GalleryIcon, hideInSimpleMode: true, featureFlag: flagModelPlaza },
      { path: '/usage?view=analysis', label: navText.value.analysis, icon: ChartIcon, hideInSimpleMode: !authStore.isObserver },
      { path: '/usage?view=records', label: navText.value.records, icon: OrderListIcon, hideInSimpleMode: !authStore.isObserver },
      { path: '/available-channels', label: t('nav.availableChannels'), icon: ChannelIcon, hideInSimpleMode: true, featureFlag: flagAvailableChannels },
      { path: '/monitor', label: t('nav.channelStatus'), icon: SignalIcon, featureFlag: flagChannelMonitor },
      { path: '/pelican-showcase', label: t('nav.pelicanShowcase'), icon: GalleryIcon, featureFlag: flagPelicanShowcase },
      { path: '/subscriptions', label: t('nav.mySubscriptions'), icon: CreditCardIcon, hideInSimpleMode: true, featureFlag: flagSubscription },
      { path: '/purchase', label: purchaseNavLabel.value, icon: RechargeSubscriptionIcon, hideInSimpleMode: true, featureFlag: flagPayment },
      { path: '/orders', label: t('nav.myOrders'), icon: OrderListIcon, hideInSimpleMode: true, featureFlag: flagPayment },
      { path: '/redeem', label: t('nav.redeem'), icon: GiftIcon, hideInSimpleMode: true },
      { path: '/affiliate', label: t('nav.affiliate'), icon: UsersIcon, hideInSimpleMode: true, featureFlag: flagAffiliate },
      { path: '/support-tickets', label: t('nav.supportTickets'), icon: SupportTicketIcon, featureFlag: flagUserSupportTickets, badge: () => supportTicketStore.userUnread },
      { path: '/security-records', label: t('securityRecords.title'), icon: ShieldIcon },
      { path: '/profile', label: centralAccountEnabled ? (locale.value.startsWith('zh') ? '业务通知设置' : 'Business notifications') : t('nav.profile'), icon: UserIcon },
      ...customMenuItemsForUser.value.map((item): NavItem => ({
        path: `/custom/${item.id}`,
        label: getCustomMenuLabel(item, locale.value),
        icon: null,
        iconSvg: item.icon_svg,
      })),
    ]
  }

  function finalizeNav(items: NavItem[]): NavItem[] {
    const visible = applyFeatureFlags(items)
    return authStore.isSimpleMode ? visible.filter(item => !item.hideInSimpleMode) : visible
  }

  const customMenuItemsForUser = computed(() => {
    const items = appStore.cachedPublicSettings?.custom_menu_items ?? []
    return items
      .filter((item) => item.visibility === 'user')
      .sort((a, b) => a.sort_order - b.sort_order)
  })

  const customMenuItemsForAdmin = computed(() => {
    return adminSettingsStore.customMenuItems
      .filter((item) => item.visibility === 'admin')
      .sort((a, b) => a.sort_order - b.sort_order)
  })

  const userNavItems = computed((): NavItem[] => finalizeNav(buildSelfNavItems()))

  const adminNavItems = computed((): NavItem[] => {
    const baseItems: NavItem[] = [
      { path: '/admin/dashboard', label: t('nav.dashboard'), icon: DashboardIcon },
      { path: '/admin/ops', label: t('nav.ops'), icon: OpsMonitoringIcon, featureFlag: flagOpsMonitoring },
      { path: '/admin/users', label: t('nav.users'), icon: UsersIcon, hideInSimpleMode: true },
      { path: '/admin/groups', label: t('nav.groups'), icon: FolderIcon },
      {
        path: '/admin/channels',
        label: t('nav.channelManagement'),
        icon: ChannelIcon,
        hideInSimpleMode: true,
        expandOnly: true,
        children: [
          { path: '/admin/channels/pricing', label: t('nav.channelPricing'), icon: PriceTagIcon },
          { path: '/admin/channels/monitor', label: t('nav.channelMonitor'), icon: SignalIcon, featureFlag: flagChannelMonitor },
        ],
      },
      { path: '/admin/subscriptions', label: t('nav.subscriptions'), icon: CreditCardIcon, hideInSimpleMode: true, featureFlag: flagSubscription },
      { path: '/admin/accounts', label: t('nav.accounts'), icon: GlobeIcon },
      { path: '/admin/smart-ops', label: t('accountOps.smartTitle'), icon: SmartOpsIcon, expandOnly: true, mergeTabs: true, children: [
        { path: '/admin/auto-config', label: t('autoConfig.title'), icon: AccountOpsIcon },
        { path: '/admin/priority-scheduling', label: t('priorityScheduling.title'), icon: AccountOpsIcon },
        { path: '/admin/channel-operations', label: t('channelOps.title'), icon: ChannelIcon },
        { path: '/admin/account-quality', label: t('qualityOps.title'), icon: QualityOpsIcon },
        { path: '/admin/controlled-experiments', label: t('controlledExperiments.title'), icon: QualityOpsIcon },
        { path: '/admin/account-ops', label: t('accountOps.title'), icon: AccountOpsIcon },
        { path: '/admin/token-guard', label: t('tokenGuard.title'), icon: TokenGuardIcon },
        { path: '/admin/token-guard-v2', label: t('tokenGuardV2.title'), icon: CredentialOpsIcon },
        { path: '/admin/pelican-tests', label: t('pelicanTests.title'), icon: PelicanTestsIcon },
        { path: '/admin/group-cache-tests', label: t('admin.groupCacheTest.title'), icon: QualityOpsIcon },
        { path: '/admin/request-captures', label: t('admin.requestCapture.title'), icon: RequestCaptureIcon, featureFlag: () => adminSettingsStore.requestCaptureEnabled },
        { path: '/admin/harvest-flow', label: t('nav.harvestFlow'), icon: FlowIcon },
      ] },
      { path: '/admin/plugins', label: t('nav.plugins'), icon: PluginIcon, featureFlag: flagPluginManagement },
      { path: '/admin/announcements', label: t('nav.announcements'), icon: BellIcon },
      { path: '/admin/support-tickets', label: t('nav.supportTickets'), icon: SupportTicketInboxIcon, featureFlag: flagSupportTickets, badge: () => supportTicketStore.adminPending },
      { path: '/admin/proxies', label: t('nav.proxies'), icon: ServerIcon },
      {
        path: '/admin/security-audit',
        label: t('nav.securityAudit'),
        icon: ShieldIcon,
        expandOnly: true,
        children: [
          { path: '/admin/risk-control', label: t('nav.contentModeration'), icon: ShieldIcon, featureFlag: flagRiskControl },
          { path: '/admin/prompt-audit', label: t('nav.promptAudit'), icon: ShieldIcon, featureFlag: flagRiskControl },
          { path: '/admin/safety-prompt-audit', label: t('admin.safetyPromptAudit.nav'), icon: ShieldIcon, featureFlag: flagRiskControl },
          { path: '/admin/ip-audit', label: t('ipAudit.title'), icon: ShieldIcon },
        ],
      },
      { path: '/admin/redeem', label: t('nav.redeemCodes'), icon: TicketIcon, hideInSimpleMode: true },
      { path: '/admin/promo-codes', label: t('nav.promoCodes'), icon: GiftIcon, hideInSimpleMode: true },
      {
        path: '/admin/affiliates',
        label: t('nav.affiliateManagement'),
        icon: UsersIcon,
        hideInSimpleMode: true,
        expandOnly: true,
        featureFlag: flagAffiliate,
        children: [
          { path: '/admin/affiliates/invites', label: t('nav.affiliateInviteRecords'), icon: UsersIcon },
          { path: '/admin/affiliates/rebates', label: t('nav.affiliateRebateRecords'), icon: OrderIcon },
          { path: '/admin/affiliates/transfers', label: t('nav.affiliateTransferRecords'), icon: CreditCardIcon },
        ],
      },
      {
        path: '/admin/orders',
        label: t('nav.orderManagement'),
        icon: OrderIcon,
        hideInSimpleMode: true,
        expandOnly: true,
        featureFlag: flagAdminPayment,
        children: [
          { path: '/admin/orders/dashboard', label: t('nav.paymentDashboard'), icon: ChartIcon },
          { path: '/admin/orders', label: t('nav.orderManagement'), icon: OrderIcon },
          { path: '/admin/orders/plans', label: t('nav.paymentPlans'), icon: CreditCardIcon },
        ],
      },
      { path: '/admin/usage', label: t('nav.usage'), icon: ChartIcon },
      { path: '/admin/audit-logs', label: t('nav.auditLogs'), icon: ShieldIcon, hideInSimpleMode: true }
    ]

    return [
      ...finalizeNav(baseItems),
      { path: '/admin/settings', label: t('nav.settings'), icon: CogIcon },
      ...customMenuItemsForAdmin.value.map((item): NavItem => ({
        path: `/custom/${item.id}`,
        label: getCustomMenuLabel(item, locale.value),
        icon: null,
        iconSvg: item.icon_svg,
      })),
    ]
  })

  const workspace = computed((): ConsoleWorkspace => {
    if (route.path.startsWith('/admin/')) return 'management'
    if (authStore.isObserver && appStore.backendModeEnabled) return 'management'
    if (isAdmin.value) {
      if (authStore.isSimpleMode) return 'management'
      const isCustomAdminPage = customMenuItemsForAdmin.value.some(item => route.path === `/custom/${item.id}`)
      const isCustomUserPage = customMenuItemsForUser.value.some(item => route.path === `/custom/${item.id}`)
      if (isCustomAdminPage && !isCustomUserPage) return 'management'
    }
    return 'personal'
  })
  const workspaceOptions = computed(() => {
    const management = { value: 'management', label: navText.value.management }
    const personal = { value: 'personal', label: navText.value.personal }
    if (isAdmin.value) return authStore.isSimpleMode ? [management] : [management, personal]
    if (authStore.isObserver) return appStore.backendModeEnabled ? [management] : [management, personal]
    return [personal]
  })
  const observerItems = computed((): NavItem[] => [
    { path: '/admin/accounts', label: t('nav.accounts'), icon: GlobeIcon },
    ...(appStore.backendModeEnabled ? [{ path: '/usage?view=records', label: navText.value.records, icon: ChartIcon }] : []),
  ])
  const visibleNavItems = computed((): NavItem[] => {
    if (authStore.isObserver && (workspace.value === 'management' || appStore.backendModeEnabled)) return observerItems.value
    if (workspace.value === 'management' && isAdmin.value) return adminNavItems.value
    if (appStore.backendModeEnabled && !isAdmin.value) return []
    return userNavItems.value
  })
  const sections = computed(() => arrangeConsoleSections(visibleNavItems.value, workspace.value, navText.value))
  const activeLocation = computed(() => resolveConsoleLocation(sections.value, route.path, route.query ?? {}))
  const searchItems = computed(() => consoleSearchTree(sections.value))
  const homePath = computed(() => (
    workspace.value === 'management' ? (isAdmin.value ? '/admin/dashboard' : '/admin/accounts') : '/dashboard'
  ))

  function permittedWorkspaceItems(value: string): NavItem[] {
    if (value === 'management') {
      if (isAdmin.value) return adminNavItems.value
      if (authStore.isObserver) return observerItems.value
      return []
    }
    return appStore.backendModeEnabled && !isAdmin.value ? [] : userNavItems.value
  }
  function workspaceStorageKey(value: string) {
    const id = authStore.user?.id
    if (!id) return undefined
    return `ritel-console:last:${id}:${authStore.user?.role}:${authStore.isSimpleMode ? 'simple' : 'standard'}:${appStore.backendModeEnabled ? 'backend' : 'full'}:${value}`
  }
  function rememberWorkspaceLocation() {
    const key = workspaceStorageKey(workspace.value)
    const value = workspaceLocation(route.path, route.query ?? {}, permittedWorkspaceItems(workspace.value))
    if (!key || !value) return
    try { sessionStorage.setItem(key, value) } catch { /* Navigation works when storage is unavailable. */ }
  }
  watch(() => [authStore.user?.id, authStore.user?.role, route.path, route.query] as const, (current, previous) => {
    if (previous && (current[0] !== previous[0] || current[1] !== previous[1])) return
    rememberWorkspaceLocation()
  }, { immediate: true, deep: true })

  function switchWorkspace(value: string) {
    if (value === workspace.value || !workspaceOptions.value.some(option => option.value === value)) return
    rememberWorkspaceLocation()
    const fallback = value === 'management' ? (isAdmin.value ? '/admin/dashboard' : '/admin/accounts') : '/dashboard'
    let saved: string | null = null
    const key = workspaceStorageKey(value)
    try { if (key) saved = sessionStorage.getItem(key) } catch { /* Use the authorized default. */ }
    const path = legalWorkspaceDestination(saved, permittedWorkspaceItems(value), fallback)
    void router.push(path)
    handleNavigate(path)
  }

  // Only the visible navigation surface owns an anchor, so IDs stay unique.
  const desktopTour = ref(typeof window === 'undefined' || window.innerWidth >= 1024)
  const updateTourViewport = () => { desktopTour.value = window.innerWidth >= 1024 }
  onMounted(() => window.addEventListener('resize', updateTourViewport))
  onUnmounted(() => window.removeEventListener('resize', updateTourViewport))
  const pathToSelector: Record<string, string> = {
    '/admin/groups': '#sidebar-group-manage',
    '/admin/accounts': '#sidebar-channel-manage',
    '/keys': '[data-tour="sidebar-my-keys"]'
  }
  function ownsTourAnchor(surface?: 'tabs' | 'drawer') {
    return !surface || (surface === 'tabs' ? desktopTour.value && !appStore.mobileOpen : appStore.mobileOpen)
  }
  function tourId(path: string, surface?: 'tabs' | 'drawer') {
    if (!ownsTourAnchor(surface)) return undefined
    return path === '/admin/accounts' ? 'sidebar-channel-manage' : path === '/admin/groups' ? 'sidebar-group-manage' : path === '/admin/redeem' ? 'sidebar-wallet' : undefined
  }
  function tourAttr(path: string, surface?: 'tabs' | 'drawer') {
    return ownsTourAnchor(surface) && path === '/keys' ? 'sidebar-my-keys' : undefined
  }
  let drawerWasOpenBeforeTour: boolean | undefined
  function finishTour() {
    if (drawerWasOpenBeforeTour === false && appStore.mobileOpen) appStore.setMobileOpen(false)
    drawerWasOpenBeforeTour = undefined
  }
  async function prepareTourTarget(selector: string) {
    const path = Object.keys(pathToSelector).find(path => pathToSelector[path] === selector)
    if (!path) return true
    updateTourViewport()
    const targetWorkspace = path.startsWith('/admin/') ? 'management' : 'personal'
    const targetSections = arrangeConsoleSections(permittedWorkspaceItems(targetWorkspace), targetWorkspace, navText.value)
    const section = targetSections.find(section => section.tabs.some(tab => tab.path === path))
    if (!section) return false
    drawerWasOpenBeforeTour ??= appStore.mobileOpen
    // The desktop strip only renders the active section. Enter that section's
    // first real page, then let the user select the highlighted destination.
    if (workspace.value !== targetWorkspace || (desktopTour.value && activeLocation.value?.section.key !== section.key)) {
      await router.push(section.tabs[0].path)
    }
    appStore.setMobileOpen(drawerWasOpenBeforeTour || !desktopTour.value || section.tabs.length === 1)
    await nextTick()
    const target = document.querySelector<HTMLElement>(selector)
    target?.scrollIntoView?.({ block: 'nearest', inline: 'nearest', behavior: 'instant' })
    const drawer = target?.closest<HTMLElement>('.console-drawer')
    if (drawer?.getAnimations) {
      await Promise.all(drawer.getAnimations().map(animation => animation.finished.catch(() => undefined)))
    }
    return true
  }
  function handleNavigate(_itemPath: string) {
    if (appStore.mobileOpen) appStore.setMobileOpen(false)
  }

  const routeMetaKeys = computed(() => resolveRouteMetaKeys(route, {
    billingMode: resolveSiteBillingMode(appStore.cachedPublicSettings),
  }))
  const pageTitle = computed(() => {
    const consoleTitle = consoleRouteTitle(route.path, route.query, locale.value, authStore.isObserver)
    if (consoleTitle) return consoleTitle
    if (route.name === 'CustomPage') {
      const id = route.params?.id as string
      const publicItems = appStore.cachedPublicSettings?.custom_menu_items ?? []
      const menuItem = publicItems.find((item) => item.id === id)
        ?? (authStore.isAdmin ? adminSettingsStore.customMenuItems.find((item) => item.id === id) : undefined)
      if (menuItem) return getCustomMenuLabel(menuItem, locale.value)
    }
    const titleKey = routeMetaKeys.value.titleKey
    if (titleKey) {
      return t(titleKey)
    }
    return (route.meta?.title as string) || ''
  })

  watch(isAdmin, (v) => { if (v) adminSettingsStore.fetch() }, { immediate: true })
  onMounted(() => { void refreshBatchImageAccess() })

  const source: ConsoleNavSource = {
    navText, userNavItems, adminNavItems, workspace, workspaceOptions, sections, activeLocation,
    searchItems, homePath, pageTitle, switchWorkspace, handleNavigate, tourId, tourAttr, prepareTourTarget, finishTour,
  }
  provide(ConsoleNavSourceKey, source)
  return source
}

// Layout provides one source; a component mounted on its own builds its own.
export function useConsoleNav(): ConsoleNavSource {
  return inject(ConsoleNavSourceKey, null) ?? provideConsoleNav()
}
