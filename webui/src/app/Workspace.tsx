import { AppShell } from '@astryxdesign/core/AppShell'
import { AlertDialog } from '@astryxdesign/core/AlertDialog'
import { Button } from '@astryxdesign/core/Button'
import { MobileNav } from '@astryxdesign/core/MobileNav'
import { SideNav, SideNavHeading, SideNavItem } from '@astryxdesign/core/SideNav'
import { StatusDot } from '@astryxdesign/core/StatusDot'
import { Component, lazy, Suspense, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { type Locale, type MessageCatalog, useMessages } from '../i18n'
import { useRuntimeStatus } from '../lib/queries'
import { navigationGuard } from '../lib/navigationGuard'
import { HomePage } from '../features/home/HomePage'
import { SessionControl } from './SessionControl'
import { getClientNavigationHref, getNavigationItem, listenForNavigation, navigateTo, navigationGroups, navigationItems } from './navigation'

const ArticleTable = lazy(() => import('../features/articles/ArticleTable').then(({ ArticleTable }) => ({ default: ArticleTable })))
const ImportPage = lazy(() => import('../features/import/ImportPage').then(({ ImportPage }) => ({ default: ImportPage })))
const ExportPage = lazy(() => import('../features/exports/ExportPage').then(({ ExportPage }) => ({ default: ExportPage })))
const LoginPage = lazy(() => import('../features/login/LoginPage').then(({ LoginPage }) => ({ default: LoginPage })))
const SettingsPage = lazy(() => import('../features/settings/SettingsPage').then(({ SettingsPage }) => ({ default: SettingsPage })))
const AccountsPage = lazy(() => import('../features/resources/ResourcePages').then(({ AccountsPage }) => ({ default: AccountsPage })))
const AlbumsPage = lazy(() => import('../features/resources/ResourcePages').then(({ AlbumsPage }) => ({ default: AlbumsPage })))
const JobsPage = lazy(() => import('../features/resources/ResourcePages').then(({ JobsPage }) => ({ default: JobsPage })))
const SavedQueriesPage = lazy(() => import('../features/resources/ResourcePages').then(({ SavedQueriesPage }) => ({ default: SavedQueriesPage })))

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
    let firstFrame = 0
    let secondFrame = 0
    const moveFocusToMain = () => {
      const main = document.getElementById('astryx-app-shell-main')
      if (!main) return
      main.tabIndex = -1
      window.cancelAnimationFrame(firstFrame)
      window.cancelAnimationFrame(secondFrame)
      firstFrame = window.requestAnimationFrame(() => {
        secondFrame = window.requestAnimationFrame(() => main.focus())
      })
    }
    const focusMain = (event: MouseEvent) => {
      if (!(event.target instanceof Element) || !event.target.closest('[data-testid="skip-to-content"], a[href="#astryx-app-shell-main"]')) return
      moveFocusToMain()
    }
    const focusMainFromKeyboard = (event: KeyboardEvent) => {
      if (event.key !== 'Enter') return
      if (!(event.target instanceof Element) || !event.target.closest('[data-testid="skip-to-content"], a[href="#astryx-app-shell-main"]')) return
      moveFocusToMain()
    }
    document.addEventListener('click', focusMain)
    document.addEventListener('keydown', focusMainFromKeyboard)
    return () => {
      window.cancelAnimationFrame(firstFrame)
      window.cancelAnimationFrame(secondFrame)
      document.removeEventListener('click', focusMain)
      document.removeEventListener('keydown', focusMainFromKeyboard)
    }
  }, [])

  const connection = getConnectionState(runtime.isSuccess, runtime.isError, messages)
  const currentPage = getNavigationItem(path)
  const navigationSections = (closeAfterNavigation = false) => navigationGroups.map((group) => {
    const items = navigationItems.filter((item) => item.group === group)
    const groupSelected = items.some((item) => path === item.href)
    return (
      <SideNavItem
        key={group}
        label={messages.navigation[group]}
        icon={<NavigationGroupIcon group={group} />}
        isSelected={groupSelected}
        collapsible={{ defaultIsCollapsed: false }}
      >
        {items.map((item) => (
          <SideNavItem
            key={item.href}
            label={messages.navigation[item.key]}
            href={item.href}
            isSelected={path === item.href}
            onClick={closeAfterNavigation ? () => setMobileNavigationOpen(false) : undefined}
          />
        ))}
      </SideNavItem>
    )
  })

  return (
    <AppShell
      className="workspace-shell"
      height="fill"
      variant="surface"
      contentPadding={4}
      mobileNav={{
        breakpoint: 'md',
        isOpen: mobileNavigationOpen,
        onOpenChange: setMobileNavigationOpen,
        content: (
          <MobileNav
            header={
              <div>
                <strong>{messages.a11y.navigation}</strong>
                <div>{messages.a11y.currentPage(messages.navigation[currentPage.key])}</div>
              </div>
            }
            label={messages.a11y.navigation}
          >
            {navigationSections(true)}
            <p className="workspace-nav-footer">{messages.product.privacy}</p>
          </MobileNav>
        )
      }}
      sideNav={
        <SideNav
          className="workspace-side-nav"
          collapsible
          header={
            <SideNavHeading
              superheading={messages.product.local}
              heading={messages.product.name}
              headingHref="/"
              subheading={messages.a11y.currentPage(messages.navigation[currentPage.key])}
            />
          }
          footer={<p className="workspace-nav-footer">{messages.product.privacy}</p>}
        >
          {navigationSections()}
        </SideNav>
      }
    >
      <header className="workspace-header workspace-command-rail">
        <div className="connection-state" role="status" aria-live="polite">
          <StatusDot variant={connection.variant} label={connection.label} isPulsing={runtime.isFetching} />
        </div>
        <div className="header-actions">
          <span className="workspace-local-note">{messages.product.localOnly}</span>
          <SessionControl messages={messages} />
          <Button
            label={messages.localeSwitch}
            variant="secondary"
            size="sm"
            onClick={() => onLocaleChange(locale === 'en' ? 'zh-CN' : 'en')}
          />
        </div>
      </header>
      <div className="workspace-content">
        <PageErrorBoundary key={path} messages={messages}>
          <Suspense fallback={<PageLoading messages={messages} />}>
            <PageFocus key={navigationID} shouldFocus={navigationID > 0}>
              {renderPage(path, locale, messages)}
            </PageFocus>
          </Suspense>
        </PageErrorBoundary>
      </div>
      <AlertDialog
        isOpen={navigationBlocked}
        onOpenChange={(isOpen) => { if (!isOpen) stayOnSettings() }}
        title={messages.settings.unsaved.title}
        description={messages.settings.unsaved.description}
        cancelLabel={messages.settings.unsaved.stay}
        actionLabel={messages.settings.unsaved.discard}
        onAction={discardAndNavigate}
      />
    </AppShell>
  )
}

function NavigationGroupIcon({ group }: { readonly group: typeof navigationGroups[number] }) {
  const path = {
    home: <><path d="M3.5 10.5 12 3l8.5 7.5" /><path d="M5.5 9.5v10h13v-10M9.5 19.5v-6h5v6" /></>,
    content: <><rect x="4" y="4" width="16" height="16" rx="2" /><path d="M8 8h8M8 12h8M8 16h5" /></>,
    work: <><path d="M8 7V5.5A1.5 1.5 0 0 1 9.5 4h5A1.5 1.5 0 0 1 16 5.5V7" /><rect x="3.5" y="7" width="17" height="12.5" rx="2" /><path d="M3.5 12h17M10 12v2h4v-2" /></>,
    system: <><circle cx="12" cy="12" r="3" /><path d="M12 2.8v2.1M12 19.1v2.1M21.2 12h-2.1M4.9 12H2.8M18.5 5.5 17 7M7 17l-1.5 1.5M18.5 18.5 17 17M7 7 5.5 5.5" /></>
  }[group]
  return <svg className="workspace-nav-group-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{path}</svg>
}

function renderPage(path: string, locale: Locale, messages: MessageCatalog) {
  if (path === '/accounts') return <AccountsPage locale={locale} messages={messages} />
  if (path === '/login') return <LoginPage messages={messages} />
  if (path === '/import') return <ImportPage messages={messages} />
  if (path === '/articles') return <ArticleTable locale={locale} messages={messages} />
  if (path === '/albums') return <AlbumsPage messages={messages} />
  if (path === '/saved-queries') return <SavedQueriesPage locale={locale} messages={messages} />
  if (path === '/jobs') return <JobsPage locale={locale} messages={messages} />
  if (path === '/exports') return <ExportPage locale={locale} messages={messages} />
  if (path === '/settings') return <SettingsPage locale={locale} messages={messages} />
  return <HomePage messages={messages} />
}

function getConnectionState(isSuccess: boolean, isError: boolean, messages: MessageCatalog) {
  if (isSuccess) return { label: messages.connection.connected, variant: 'success' as const }
  if (isError) return { label: messages.connection.unavailable, variant: 'error' as const }
  return { label: messages.connection.checking, variant: 'neutral' as const }
}

function PageLoading({ messages }: { readonly messages: MessageCatalog }) {
  return <p role="status" aria-live="polite">{messages.connection.checking}</p>
}

function PageFocus({ children, shouldFocus }: { readonly children: ReactNode; readonly shouldFocus: boolean }) {
  useLayoutEffect(() => {
    if (!shouldFocus) return
    let timeout = 0
    let attempts = 0
    const focusHeading = () => {
      const heading = document.querySelector<HTMLElement>('#astryx-app-shell-main h1')
      if (heading) {
        heading.tabIndex = -1
        heading.focus()
        return
      }
      attempts += 1
      if (attempts < 20) timeout = window.setTimeout(focusHeading, 25)
    }
    timeout = window.setTimeout(focusHeading, 0)
    return () => window.clearTimeout(timeout)
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
