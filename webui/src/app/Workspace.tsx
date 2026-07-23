import { AppShell } from '@astryxdesign/core/AppShell'
import { Button } from '@astryxdesign/core/Button'
import { SideNav, SideNavHeading, SideNavItem, SideNavSection } from '@astryxdesign/core/SideNav'
import { StatusDot } from '@astryxdesign/core/StatusDot'
import { useEffect, useState } from 'react'
import { ArticleTable } from '../features/articles/ArticleTable'
import { type Locale, type MessageCatalog, useMessages } from '../i18n'
import { useRuntimeStatus } from '../lib/queries'

interface WorkspaceProps {
  readonly locale: Locale
  readonly onLocaleChange: (locale: Locale) => void
}

const navigation = [
  { group: 'workspace', href: '/', key: 'overview' },
  { group: 'library', href: '/articles', key: 'articles' }
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
          {(['workspace', 'library'] as const).map((group) => (
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
          <Button
            label={messages.localeSwitch}
            variant="secondary"
            size="sm"
            onClick={() => onLocaleChange(locale === 'en' ? 'zh-CN' : 'en')}
          />
        </div>
      </header>
      <main id="workspace-content" className="workspace-content" tabIndex={-1}>
        {path === '/articles' ? <ArticleTable locale={locale} messages={messages} /> : <Overview messages={messages} />}
      </main>
    </AppShell>
  )
}

function getConnectionState(isSuccess: boolean, isError: boolean, messages: MessageCatalog) {
  if (isSuccess) return { label: messages.connection.connected, variant: 'success' as const }
  if (isError) return { label: messages.connection.unavailable, variant: 'error' as const }
  return { label: messages.connection.checking, variant: 'neutral' as const }
}

function Overview({ messages }: { readonly messages: MessageCatalog }) {
  return (
    <section className="overview" aria-labelledby="overview-title">
      <p className="eyebrow">{messages.product.local}</p>
      <h1 id="overview-title">{messages.overview.title}</h1>
      <p className="lede">{messages.overview.description}</p>
      <div className="overview-grid">
        <section className="workspace-panel" aria-labelledby="profile-title">
          <h2 id="profile-title">{messages.overview.profileTitle}</h2>
          <p>{messages.overview.profileDescription}</p>
        </section>
        <section className="workspace-panel" aria-labelledby="next-title">
          <h2 id="next-title">{messages.overview.nextTitle}</h2>
          <p>{messages.overview.nextDescription}</p>
        </section>
      </div>
    </section>
  )
}
