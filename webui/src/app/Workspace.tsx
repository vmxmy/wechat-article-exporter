import { AppShell } from '@astryxdesign/core/AppShell'
import { Button } from '@astryxdesign/core/Button'
import { MobileNav } from '@astryxdesign/core/MobileNav'
import { SideNav, SideNavHeading, SideNavItem, SideNavSection } from '@astryxdesign/core/SideNav'
import { StatusDot } from '@astryxdesign/core/StatusDot'
import { Component, lazy, Suspense, useEffect, useLayoutEffect, useState, type ReactNode } from 'react'
import { type Locale, type MessageCatalog, useMessages } from '../i18n'
import { useRuntimeStatus } from '../lib/queries'
import { HomePage } from '../features/home/HomePage'
import { SessionControl } from './SessionControl'
import { getNavigationItem, navigationEvent, navigationGroups, navigationItems } from './navigation'

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
  const runtime = useRuntimeStatus()

  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  useEffect(() => {
    const updatePath = () => {
      setPath(window.location.pathname)
      setNavigationID((current) => current + 1)
    }
    window.addEventListener('popstate', updatePath)
    window.addEventListener(navigationEvent, updatePath)
    return () => {
      window.removeEventListener('popstate', updatePath)
      window.removeEventListener(navigationEvent, updatePath)
    }
  }, [])

  useEffect(() => {
    let firstFrame = 0
    let secondFrame = 0
    const focusMain = (event: MouseEvent) => {
      if (!(event.target instanceof Element) || !event.target.closest('[data-testid="skip-to-content"]')) return
      const main = document.getElementById('astryx-app-shell-main')
      if (!main) return
      main.tabIndex = -1
      window.cancelAnimationFrame(firstFrame)
      window.cancelAnimationFrame(secondFrame)
      firstFrame = window.requestAnimationFrame(() => {
        secondFrame = window.requestAnimationFrame(() => main.focus())
      })
    }
    document.addEventListener('click', focusMain)
    return () => {
      window.cancelAnimationFrame(firstFrame)
      window.cancelAnimationFrame(secondFrame)
      document.removeEventListener('click', focusMain)
    }
  }, [])

  const connection = getConnectionState(runtime.isSuccess, runtime.isError, messages)
  const currentPage = getNavigationItem(path)
  const navigationSections = (closeAfterNavigation = false) => navigationGroups.map((group) => (
    <SideNavSection key={group} title={messages.navigation[group]}>
      {navigationItems.filter((item) => item.group === group).map((item) => (
        <SideNavItem
          key={item.href}
          label={messages.navigation[item.key]}
          href={item.href}
          isSelected={path === item.href}
          onClick={closeAfterNavigation ? () => setMobileNavigationOpen(false) : undefined}
        />
      ))}
    </SideNavSection>
  ))

  return (
    <AppShell
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
      <header className="workspace-header">
        <div className="connection-state" role="status" aria-live="polite">
          <StatusDot variant={connection.variant} label={connection.label} isPulsing={runtime.isFetching} />
          <span>{connection.label}</span>
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
    </AppShell>
  )
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
