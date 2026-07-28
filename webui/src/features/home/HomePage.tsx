import { Link } from '@/components/controls/Link'
import { ActionGroup, DefinitionList, PageHeader, PageStack, Panel } from '../../components/presentation'
import type { Locale, MessageCatalog } from '../../i18n'
import { formatDateTime, formatJobKind } from '../../lib/presentation'
import { useWorkspaceSnapshot } from '../../lib/queries'

interface HomeAction {
  readonly title: string
  readonly description: string
  readonly primary: { readonly href: string; readonly label: string }
  readonly secondary?: { readonly href: string; readonly label: string }
}

export function HomePage({ messages, locale }: { readonly messages: MessageCatalog; readonly locale: Locale }) {
  const snapshot = useWorkspaceSnapshot()
  const runtime = snapshot.data?.runtime
  const session = snapshot.data?.session
  const storage = snapshot.data?.storage ?? runtime?.storage
  const failedJobs = countFailedJobs(snapshot.data?.jobs)
  const action = getHomeAction(messages, session?.state, storage?.accounts, storage?.articles, failedJobs)
  const recentJobs = snapshot.data?.jobs ? takeRecentJobs(snapshot.data.jobs, 4) : []

  return (
    <PageStack className="overview" aria-labelledby="overview-title">
      <PageHeader eyebrow={messages.navigation.home} title={messages.overview.title} titleId="overview-title" description={messages.overview.description} />

      {snapshot.isLoading ? <p role="status">{messages.connection.checking}</p> : null}
      {snapshot.isError ? <p role="alert">{messages.overview.unavailable}</p> : null}

      {!snapshot.isLoading && !snapshot.isError && action ? (
        <Panel className={`overview-primary-action${failedJobs > 0 ? ' overview-primary-action-warning' : ''}`} aria-labelledby="next-action-title">
          <p className="overview-action-label">{messages.overview.primaryActionTitle}</p>
          <h2 id="next-action-title">{action.title}</h2>
          <p>{action.description}</p>
          <ActionGroup className="overview-action-cluster" align="start" gap="cluster" stackAt="compact">
            <Link href={action.primary.href} isStandalone hasUnderline>{action.primary.label}</Link>
            {action.secondary ? <Link href={action.secondary.href} isStandalone>{action.secondary.label}</Link> : null}
          </ActionGroup>
        </Panel>
      ) : null}

      {!snapshot.isLoading && !snapshot.isError ? (
        <Panel className="overview-quick-entries" tone="muted" aria-labelledby="quick-entries-title">
          <h2 id="quick-entries-title">{messages.overview.secondaryActionsTitle}</h2>
          <ActionGroup className="overview-quick-cluster" align="start" gap="cluster" stackAt="compact">
            <Link href="/articles" isStandalone>{messages.overview.quickEntries.browseArticles}</Link>
            <Link href="/exports" isStandalone>{messages.overview.quickEntries.startExport}</Link>
            <Link href="/accounts" isStandalone>{messages.overview.quickEntries.manageAccounts}</Link>
            <Link href="/jobs" isStandalone>{messages.overview.quickEntries.reviewTasks}</Link>
          </ActionGroup>
        </Panel>
      ) : null}

      <div className="overview-grid">
        <section className="overview-fact" aria-labelledby="session-title">
          <h2 id="session-title">{messages.overview.sessionTitle}</h2>
          <DefinitionList
            labelWidth="5.5rem"
            items={[
              { term: messages.overview.sessionAccount, description: session?.accountName ?? '—' },
              { term: messages.overview.sessionState, description: session ? messages.login.states[session.state] ?? session.state : '—' }
            ]}
          />
        </section>
        <section className="overview-fact overview-fact-storage" aria-labelledby="storage-title">
          <h2 id="storage-title">{messages.overview.storageTitle}</h2>
          <p>{storage ? messages.overview.storageCounts(storage.accounts, storage.articles, storage.albums, storage.jobs) : '—'}</p>
        </section>
        <section className="overview-fact overview-fact-technical" aria-labelledby="runtime-title">
          <h2 id="runtime-title">{messages.overview.runtimeTitle}</h2>
          <DefinitionList
            labelWidth="5.5rem"
            items={[
              { term: messages.overview.runtimeProfile, description: runtime?.profileId ?? runtime?.profile ?? '—' },
              { term: messages.overview.runtimeVersion, description: runtime?.version ?? '—' }
            ]}
          />
        </section>
      </div>

      {!snapshot.isLoading && !snapshot.isError ? (
        <Panel className="overview-recent-jobs" tone="muted" aria-labelledby="recent-jobs-title">
          <h2 id="recent-jobs-title">{messages.overview.recentJobsTitle}</h2>
          {recentJobs.length === 0 ? <p>{messages.overview.recentJobsEmpty}</p> : (
            <ul className="overview-recent-jobs-list">
              {recentJobs.map((job) => (
                <li key={job.id}>
                  <span className="overview-recent-job-label">{formatJobKind(job.kind, locale).label}</span>
                  <span className="overview-recent-job-state">{job.state}</span>
                  <span className="overview-recent-job-time">{formatDateTime(job.updatedAt ?? job.createdAt, locale)}</span>
                </li>
              ))}
            </ul>
          )}
          <ActionGroup align="start" gap="cluster"><Link href="/jobs" isStandalone hasUnderline>{messages.overview.quickEntries.reviewTasks}</Link></ActionGroup>
        </Panel>
      ) : null}
    </PageStack>
  )
}

function countFailedJobs(jobs: { readonly data?: readonly { readonly state: string }[]; readonly items?: readonly { readonly state: string }[] } | undefined): number {
  const entries = jobs?.data ?? jobs?.items ?? []
  return entries.filter((job) => job.state === 'failed').length
}

interface RecentJob {
  readonly id: string
  readonly kind: string
  readonly state: string
  readonly createdAt?: string
  readonly updatedAt?: string
}

function takeRecentJobs(jobs: { readonly data?: readonly RecentJob[]; readonly items?: readonly RecentJob[] }, limit: number): readonly RecentJob[] {
  const entries = jobs.data ?? jobs.items ?? []
  return [...entries]
    .sort((left, right) => compareISOTime(right.updatedAt ?? right.createdAt, left.updatedAt ?? left.createdAt))
    .slice(0, limit)
}

function compareISOTime(left: string | undefined, right: string | undefined): number {
  const leftTime = left ? Date.parse(left) : NaN
  const rightTime = right ? Date.parse(right) : NaN
  if (Number.isNaN(leftTime) && Number.isNaN(rightTime)) return 0
  if (Number.isNaN(leftTime)) return 1
  if (Number.isNaN(rightTime)) return -1
  return leftTime - rightTime
}

function getHomeAction(messages: MessageCatalog, sessionState?: string, accounts = 0, articles = 0, failedJobs = 0): HomeAction | undefined {
  const copy = messages.overview.actions
  if (sessionState !== 'authenticated') {
    return { title: copy.signInTitle, description: copy.signInDescription, primary: { href: '/login', label: copy.signIn } }
  }
  if (failedJobs > 0) {
    return { title: copy.failedJobsTitle(failedJobs), description: copy.failedJobsDescription, primary: { href: '/jobs', label: copy.failedJobs } }
  }
  if (accounts === 0) {
    return { title: copy.addAccountTitle, description: copy.addAccountDescription, primary: { href: '/accounts', label: copy.addAccount } }
  }
  if (articles === 0) {
    return { title: copy.syncTitle, description: copy.syncDescription, primary: { href: '/accounts', label: copy.sync } }
  }
  return {
    title: copy.browseTitle,
    description: copy.browseDescription,
    primary: { href: '/articles', label: copy.browse },
    secondary: { href: '/exports', label: copy.export }
  }
}
