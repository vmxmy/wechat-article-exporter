import { ConfirmDialog } from '@/components/controls/ConfirmDialog'
import { Button } from '@/components/controls/Button'
import { StatusDot } from '@/components/controls/StatusDot'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarInset,
  SidebarProvider,
  SidebarTrigger
} from '@/components/ui/sidebar'
import { Icons } from '@/components/icons'
import { Component, lazy, Suspense, useEffect, useLayoutEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from 'react'
import { type Locale, type MessageCatalog, useMessages } from '../i18n'
import { useRuntimeStatus } from '../lib/queries'
import { useWorkspaceSessionExpired } from '../lib/workspaceSession'
import { navigationGuard } from '../lib/navigationGuard'
import { HomePage } from '../features/home/HomePage'
import { SessionControl } from './SessionControl'
import { getClientNavigationHref, listenForNavigation, navigateTo, navigationGroups, navigationItems } from './navigation'
import { matchRoute } from './routes'

const ArticleTable = lazy(() => import('../features/articles/ArticleTable').then(({ ArticleTable }) => ({ default: ArticleTable })))
const ImportPage = lazy(() => import('../features/import/ImportPage').then(({ ImportPage }) => ({ default: ImportPage })))
const ExportPage = lazy(() => import('../features/exports/ExportPage').then(({ ExportPage }) => ({ default: ExportPage })))
const LoginPage = lazy(() => import('../features/login/LoginPage').then(({ LoginPage }) => ({ default: LoginPage })))
const SettingsPage = lazy(() => import('../features/settings/SettingsPage').then(({ SettingsPage }) => ({ default: SettingsPage })))
const AccountsPage = lazy(() => import('../features/resources/ResourcePages').then(({ AccountsPage }) => ({ default: AccountsPage })))
const AlbumsPage = lazy(() => import('../features/resources/ResourcePages').then(({ AlbumsPage }) => ({ default: AlbumsPage })))
const JobsPage = lazy(() => import('../features/resources/ResourcePages').then(({ JobsPage }) => ({ default: JobsPage })))
const SavedQueriesPage = lazy(() => import('../features/resources/ResourcePages').then(({ SavedQueriesPage }) => ({ default: SavedQueriesPage })))
const NotFoundPage = lazy(() => import('../features/NotFoundPage').then(({ NotFoundPage }) => ({ default: NotFoundPage })))

interface WorkspaceProps {
  readonly locale: Locale
  readonly onLocaleChange: (locale: Locale) => void
}

export function Workspace({ locale, onLocaleChange }: WorkspaceProps) {
  const messages = useMessages(locale)
  const [path, setPath] = useState(window.location.pathname)
  const [navigationID, setNavigationID] = useState(0)
  const [mobileNavigationOpen, setMobileNavigationOpen] = useState(false)
  const [navigationBlocked, setNavigationBlocked] = useState(navigationGuard.hasPendingNavigation())
  const blockedNavigationTrigger = useRef<HTMLElement | null>(null)
  const runtime = useRuntimeStatus()

  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  useEffect(() => {
    const updatePath = () => {
      setPath(window.location.pathname)
      setNavigationID((current) => current + 1)
      setMobileNavigationOpen(false)
      blockedNavigationTrigger.current = null
    }
    return listenForNavigation(updatePath)
  }, [])

  useEffect(() => navigationGuard.subscribe(() => {
    const blocked = navigationGuard.hasPendingNavigation()
    if (blocked && !blockedNavigationTrigger.current && document.activeElement instanceof HTMLElement) {
      blockedNavigationTrigger.current = document.activeElement
    }
    setNavigationBlocked(blocked)
  }), [])


  useEffect(() => {
    const interceptWorkspaceLink = (event: MouseEvent) => {
      if (!(event.target instanceof Element)) return
      const anchor = event.target.closest<HTMLAnchorElement>('a[href]')
      if (!anchor || anchor.hasAttribute('data-wechat-router-link')) return
      const href = getClientNavigationHref(anchor, event)
      if (!href) return
      event.preventDefault()
      blockedNavigationTrigger.current = anchor
      navigateTo(href)
    }
    document.addEventListener('click', interceptWorkspaceLink)
    return () => document.removeEventListener('click', interceptWorkspaceLink)
  }, [])

  const stayOnSettings = () => {
    navigationGuard.stay()
    const trigger = blockedNavigationTrigger.current
    blockedNavigationTrigger.current = null
    window.requestAnimationFrame(() => trigger?.focus())
  }

  const discardAndNavigate = () => {
    navigationGuard.discard()
    blockedNavigationTrigger.current = null
  }

  useEffect(() => {
    let timeout = 0
    const moveFocusToMain = () => {
      window.clearTimeout(timeout)
      let attempts = 0
      const claimFocus = () => {
        const main = document.getElementById('astryx-app-shell-main')
        if (!main) return
        if (document.activeElement === main) return
        main.tabIndex = -1
        main.focus()
        attempts += 1
        if (attempts < 20) timeout = window.setTimeout(claimFocus, 25)
      }
      timeout = window.setTimeout(claimFocus, 0)
    }
    const isSkipLink = (target: EventTarget | null) =>
      target instanceof Element && target.closest('[data-testid="skip-to-content"], a[href="#astryx-app-shell-main"]')
    const focusMain = (event: MouseEvent) => {
      if (!isSkipLink(event.target)) return
      event.preventDefault()
      moveFocusToMain()
    }
    const focusMainFromKeyboard = (event: KeyboardEvent) => {
      if (event.key !== 'Enter') return
      if (!isSkipLink(event.target)) return
      event.preventDefault()
      moveFocusToMain()
    }
    document.addEventListener('click', focusMain)
    document.addEventListener('keydown', focusMainFromKeyboard)
    return () => {
      window.clearTimeout(timeout)
      document.removeEventListener('click', focusMain)
      document.removeEventListener('keydown', focusMainFromKeyboard)
    }
  }, [])

  const connection = getConnectionState(runtime.isSuccess, runtime.isError, messages)
  const matchedRoute = matchRoute(path)
  const pageLabel = resolvePageLabel(matchedRoute, path, messages)
  useDocumentTitle(path, matchedRoute, messages)

  const navigateFromSidebar = (event: ReactMouseEvent<HTMLAnchorElement>) => {
    const target = getClientNavigationHref(event.currentTarget, event.nativeEvent)
    if (!target) {
      if (
        event.button === 0
        && !event.metaKey
        && !event.ctrlKey
        && !event.shiftKey
        && !event.altKey
        && event.currentTarget.pathname === window.location.pathname
        && event.currentTarget.search === window.location.search
      ) {
        event.preventDefault()
        setMobileNavigationOpen(false)
      }
      return
    }
    event.preventDefault()
    blockedNavigationTrigger.current = event.currentTarget
    navigateTo(target)
  }

  const renderNavGroups = () => navigationGroups.map((group) => {
    const items = navigationItems.filter((item) => item.group === group)
    return (
      <Collapsible key={group} defaultOpen className="workspace-nav-group">
        <CollapsibleTrigger className="workspace-nav-group-trigger">
          <NavigationGroupIcon group={group} />
          <span>{messages.navigation[group]}</span>
          <Icons.chevronDown className="workspace-nav-group-chev" aria-hidden="true" />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <ul className="workspace-nav-items">
            {items.map((item) => (
              <li key={item.href}>
                <a
                  href={item.href}
                  aria-current={path === item.href ? 'page' : undefined}
                  className="workspace-nav-item"
                  data-wechat-router-link="true"
                  onClick={navigateFromSidebar}
                >
                  {messages.navigation[item.key]}
                </a>
              </li>
            ))}
          </ul>
        </CollapsibleContent>
      </Collapsible>
    )
  })

  const sideNavHeader = (
    <div className="workspace-side-nav-heading">
      <div className="workspace-side-nav-super">{messages.product.local}</div>
      <a href="/" className="workspace-side-nav-title" data-wechat-router-link="true" onClick={navigateFromSidebar}>{messages.product.name}</a>
      <div className="workspace-side-nav-sub">{messages.a11y.currentPage(pageLabel)}</div>
    </div>
  )

  return (
    <SidebarProvider
      mobileOpen={mobileNavigationOpen}
      onMobileOpenChange={setMobileNavigationOpen}
      persistOpenState={false}
      keyboardShortcut={false}
      mobileTitle={messages.a11y.navigation}
      mobileDescription={messages.a11y.currentPage(pageLabel)}
      openLabel={messages.a11y.openNavigation}
      closeLabel={messages.a11y.closeNavigation}
      className="workspace-shell"
    >
      <a href="#astryx-app-shell-main" className="workspace-skip-link" data-testid="skip-to-content" onClick={(event) => { event.preventDefault(); document.getElementById('astryx-app-shell-main')?.focus() }}>{messages.a11y.skip}</a>
      <Sidebar collapsible="offcanvas" className="workspace-side-nav">
        <SidebarHeader>{sideNavHeader}</SidebarHeader>
        <SidebarContent>
          <nav aria-label={messages.a11y.navigation} className="workspace-side-nav-body">
            {renderNavGroups()}
          </nav>
        </SidebarContent>
        <SidebarFooter>
          <p className="workspace-nav-footer">{messages.product.privacy}</p>
        </SidebarFooter>
      </Sidebar>

      <SidebarInset id="astryx-app-shell-main" className="workspace-main-column">
        <header className="workspace-header workspace-command-rail">
          <div className="workspace-header-inner">
            <SidebarTrigger target="mobile" className="workspace-mobile-nav-toggle">
              <Icons.menu2 className="size-5" aria-hidden="true" />
            </SidebarTrigger>
            <div className="connection-state" role="status" aria-live="polite">
              <StatusDot variant={connection.variant} label={connection.label} isPulsing={runtime.isFetching} />
            </div>
            <div className="header-actions">
              <span className="workspace-local-note">{messages.product.localOnly}</span>
              <Button
                label={messages.navigation.importAction}
                variant="secondary"
                size="sm"
                onClick={() => navigateTo('/import')}
              />
              <SessionControl messages={messages} />
              <Button
                label={messages.localeSwitch}
                variant="secondary"
                size="sm"
                onClick={() => onLocaleChange(locale === 'en' ? 'zh-CN' : 'en')}
              />
            </div>
          </div>
        </header>
        <div className="workspace-content">
          <WorkspaceSessionNotice messages={messages} />
          <PageErrorBoundary key={path} messages={messages}>
            <Suspense fallback={<PageLoading messages={messages} />}>
              <PageFocus key={navigationID} shouldFocus={navigationID > 0}>
                {renderPage(path, locale, messages)}
              </PageFocus>
            </Suspense>
          </PageErrorBoundary>
        </div>
      </SidebarInset>

      <ConfirmDialog
        isOpen={navigationBlocked}
        onOpenChange={(isOpen) => { if (!isOpen) stayOnSettings() }}
        title={messages.settings.unsaved.title}
        description={messages.settings.unsaved.description}
        closeLabel={messages.a11y.closeDialog}
        cancelLabel={messages.settings.unsaved.stay}
        actionLabel={messages.settings.unsaved.discard}
        onAction={discardAndNavigate}
      />
    </SidebarProvider>
  )
}

function NavigationGroupIcon({ group }: { readonly group: typeof navigationGroups[number] }) {
  const path = {
    home: <><path d="M3.5 10.5 12 3l8.5 7.5" /><path d="M5.5 9.5v10h13v-10M9.5 19.5v-6h5v6" /></>,
    content: <><rect x="4" y="4" width="16" height="16" rx="2" /><path d="M8 8h8M8 12h8M8 16h5" /></>,
    tasks: <><path d="M8 7V5.5A1.5 1.5 0 0 1 9.5 4h5A1.5 1.5 0 0 1 16 5.5V7" /><rect x="3.5" y="7" width="17" height="12.5" rx="2" /><path d="M3.5 12h17M10 12v2h4v-2" /></>,
    settings: <><circle cx="12" cy="12" r="3" /><path d="M12 2.8v2.1M12 19.1v2.1M21.2 12h-2.1M4.9 12H2.8M18.5 5.5 17 7M7 17l-1.5 1.5M18.5 18.5 17 17M7 7 5.5 5.5" /></>
  }[group]
  return <svg className="workspace-nav-group-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{path}</svg>
}

function renderPage(path: string, locale: Locale, messages: MessageCatalog) {
  const route = matchRoute(path)
  switch (route?.key) {
    case 'overview': return <HomePage messages={messages} locale={locale} />
    case 'login': return <LoginPage messages={messages} />
    case 'import': return <ImportPage messages={messages} />
    case 'accounts': return <AccountsPage locale={locale} messages={messages} />
    case 'articles': return <ArticleTable locale={locale} messages={messages} />
    case 'albums': return <AlbumsPage locale={locale} messages={messages} />
    case 'saved-queries': return <SavedQueriesPage locale={locale} messages={messages} />
    case 'jobs': return <JobsPage locale={locale} messages={messages} />
    case 'exports': return <ExportPage locale={locale} messages={messages} />
    case 'settings': return <SettingsPage locale={locale} messages={messages} />
    default: return <NotFoundPage messages={messages} />
  }
}

function getConnectionState(isSuccess: boolean, isError: boolean, messages: MessageCatalog) {
  if (isSuccess) return { label: messages.connection.connected, variant: 'success' as const }
  if (isError) return { label: messages.connection.unavailable, variant: 'error' as const }
  return { label: messages.connection.checking, variant: 'neutral' as const }
}

function resolvePageLabel(route: ReturnType<typeof matchRoute>, path: string, messages: MessageCatalog): string {
  if (!route) return messages.notFound.title
  if (path === '/') return messages.navigation.home
  return messages.navigation[route.titleKey]
}

function useDocumentTitle(path: string, route: ReturnType<typeof matchRoute>, messages: MessageCatalog) {
  useEffect(() => {
    if (!route) {
      document.title = messages.documentTitle.notFound
      return
    }
    if (path === '/') {
      document.title = messages.documentTitle.appName
      return
    }
    document.title = messages.documentTitle.page(messages.navigation[route.titleKey])
  }, [path, route, messages])
}

// The workspace cookie is minted once from a one-time bootstrap URL, so an
// expired one cannot be recovered from inside the page. Say so explicitly:
// otherwise every request fails with an authentication error and the only
// visible session state is the WeChat one, which is not the broken one.
function WorkspaceSessionNotice({ messages }: { readonly messages: MessageCatalog }) {
  const isExpired = useWorkspaceSessionExpired()
  if (!isExpired) return null
  return (
    <div className="workspace-session-notice" role="alert">
      <strong>{messages.login.workspaceExpiredTitle}</strong>
      <p>{messages.login.workspaceExpiredDescription}</p>
    </div>
  )
}

function PageLoading({ messages }: { readonly messages: MessageCatalog }) {
  return <p role="status" aria-live="polite">{messages.connection.checking}</p>
}

function PageFocus({ children, shouldFocus }: { readonly children: ReactNode; readonly shouldFocus: boolean }) {
  useLayoutEffect(() => {
    if (!shouldFocus) return
    let settled = false
    let observer: MutationObserver | null = null
    let timeout = 0
    const focusHeading = () => {
      if (settled) return
      const heading = document.querySelector<HTMLElement>('#astryx-app-shell-main h1')
      if (!heading) return false
      settled = true
      heading.tabIndex = -1
      heading.focus()
      return true
    }
    if (focusHeading()) return
    observer = new MutationObserver(() => { if (focusHeading()) cleanup() })
    observer.observe(document.body, { childList: true, subtree: true })
    timeout = window.setTimeout(cleanup, 3_000)
    function cleanup() {
      observer?.disconnect()
      observer = null
      window.clearTimeout(timeout)
    }
    return cleanup
  }, [shouldFocus])

  return <>{children}</>
}

class PageErrorBoundary extends Component<{ readonly children: ReactNode; readonly messages: MessageCatalog }, { readonly hasError: boolean }> {
  state = { hasError: false }

  static getDerivedStateFromError() {
    return { hasError: true }
  }

  render() {
    if (this.state.hasError) {
      return (
        <section className="error-state" role="alert">
          <p>{this.props.messages.connection.unavailable}</p>
          <Button label={this.props.messages.settings.retry} variant="secondary" onClick={() => window.location.reload()} />
        </section>
      )
    }
    return this.props.children
  }
}
