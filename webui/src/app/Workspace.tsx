import { AppShell } from '@astryxdesign/core/AppShell'
import { Button } from '@astryxdesign/core/Button'
import { SideNav, SideNavHeading, SideNavItem, SideNavSection } from '@astryxdesign/core/SideNav'
import { StatusDot } from '@astryxdesign/core/StatusDot'
import { useEffect, useState } from 'react'
import { ArticleTable } from '../features/articles/ArticleTable'
import { ImportPage } from '../features/import/ImportPage'
import { LoginPage } from '../features/login/LoginPage'
import { AccountsPage, AlbumsPage, JobsPage, SavedQueriesPage } from '../features/resources/ResourcePages'
import { type Locale, type MessageCatalog, useMessages } from '../i18n'
import { useRuntimeStatus, useWorkspaceSnapshot } from '../lib/queries'

interface WorkspaceProps {
  readonly locale: Locale
  readonly onLocaleChange: (locale: Locale) => void
}

const navigation = [
  { group: 'workspace', href: '/', key: 'overview' },
  { group: 'workspace', href: '/login', key: 'login' },
  { group: 'library', href: '/accounts', key: 'accounts' },
  { group: 'library', href: '/articles', key: 'articles' },
  { group: 'library', href: '/albums', key: 'albums' },
  { group: 'library', href: '/saved-queries', key: 'savedQueries' },
  { group: 'operations', href: '/jobs', key: 'jobs' },
  { group: 'operations', href: '/import', key: 'import' }
] as const

export function Workspace({ locale, onLocaleChange }: WorkspaceProps) {
  const messages = useMessages(locale)
  const [path, setPath] = useState(window.location.pathname)
  const runtime = useRuntimeStatus()

  useEffect(() => {
    const updatePath = () => setPath(window.location.pathname)
    window.addEventListener('popstate', updatePath)
    return () => window.removeEventListener('popstate', updatePath)
  }, [])

  const connection = getConnectionState(runtime.isSuccess, runtime.isError, messages)

  return (
    <AppShell
      height="fill"
      variant="surface"
      contentPadding={4}
      sideNav={
        <SideNav
          collapsible
          header={<SideNavHeading superheading={messages.product.local} heading={messages.product.name} headingHref="/" />}
          footer={<p className="workspace-nav-footer">{messages.product.privacy}</p>}
        >
          {(['workspace', 'library', 'operations'] as const).map((group) => (
            <SideNavSection key={group} title={messages.navigation[group]}>
              {navigation.filter((item) => item.group === group).map((item) => (
                <SideNavItem
                  key={item.href}
                  label={messages.navigation[item.key]}
                  href={item.href}
                  isSelected={path === item.href}
                />
              ))}
            </SideNavSection>
          ))}
        </SideNav>
      }
    >
      <a className="skip-link" href="#workspace-content">{messages.a11y.skip}</a>
      <header className="workspace-header">
        <div className="connection-state" role="status" aria-live="polite">
          <StatusDot variant={connection.variant} label={connection.label} isPulsing={runtime.isFetching} />
          <span>{connection.label}</span>
        </div>
        <div className="header-actions">
          <span className="read-only-badge">{messages.product.beta} · {messages.product.readOnly}</span>
          <Button
            label={messages.localeSwitch}
            variant="secondary"
            size="sm"
            onClick={() => onLocaleChange(locale === 'en' ? 'zh-CN' : 'en')}
          />
        </div>
      </header>
      <main id="workspace-content" className="workspace-content" tabIndex={-1}>
        {renderPage(path, locale, messages)}
      </main>
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
  return <Overview messages={messages} />
}

function getConnectionState(isSuccess: boolean, isError: boolean, messages: MessageCatalog) {
  if (isSuccess) return { label: messages.connection.connected, variant: 'success' as const }
  if (isError) return { label: messages.connection.unavailable, variant: 'error' as const }
  return { label: messages.connection.checking, variant: 'neutral' as const }
}

function Overview({ messages }: { readonly messages: MessageCatalog }) {
  const snapshot = useWorkspaceSnapshot()
  const runtime = snapshot.data?.runtime
  const session = snapshot.data?.session
  const storage = snapshot.data?.storage ?? runtime?.storage
  return (
    <section className="overview" aria-labelledby="overview-title">
      <p className="eyebrow">{messages.product.local}</p>
      <h1 id="overview-title">{messages.overview.title}</h1>
      <p className="lede">{messages.overview.description}</p>
      <div className="overview-grid">
        <section className="workspace-panel" aria-labelledby="profile-title">
          <h2 id="profile-title">{messages.overview.profileTitle}</h2>
          {snapshot.isLoading ? <p role="status">{messages.connection.checking}</p> : null}
          {snapshot.isError ? <p>{messages.overview.unavailable}</p> : <dl className="facts-list"><div><dt>{messages.overview.runtimeProfile}</dt><dd>{runtime?.profileId ?? runtime?.profile ?? '—'}</dd></div><div><dt>{messages.overview.runtimeVersion}</dt><dd>{runtime?.version ?? '—'}</dd></div></dl>}
        </section>
        <section className="workspace-panel" aria-labelledby="next-title">
          <h2 id="next-title">{messages.overview.sessionTitle}</h2>
          {snapshot.isError ? <p>{messages.overview.unavailable}</p> : <dl className="facts-list"><div><dt>{messages.overview.sessionAccount}</dt><dd>{session?.accountName ?? '—'}</dd></div><div><dt>{messages.overview.sessionState}</dt><dd>{session?.state ?? runtime?.session ?? '—'}</dd></div></dl>}
        </section>
        <section className="workspace-panel" aria-labelledby="storage-title">
          <h2 id="storage-title">{messages.overview.storageTitle}</h2>
          <p>{storage ? messages.overview.storageCounts(storage.accounts, storage.articles, storage.albums, storage.jobs) : messages.overview.unavailable}</p>
        </section>
        <section className="workspace-panel" aria-labelledby="next-steps-title">
          <h2 id="next-steps-title">{messages.overview.nextTitle}</h2>
          <p>{messages.overview.nextDescription}</p>
        </section>
      </div>
    </section>
  )
}
