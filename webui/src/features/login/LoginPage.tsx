import { ConfirmDialog } from '@/components/controls/ConfirmDialog'
import { Button } from '@/components/controls/Button'
import { Selector } from '@/components/controls/Selector'
import { useCallback, useEffect, useRef, useState } from 'react'
import { ActionGroup, DefinitionList, PageHeader, PageStack, Panel } from '../../components/presentation'
import type { MessageCatalog } from '../../i18n'
import { useSessionStatus, useSwitchableAccounts, useWorkspaceMutations } from '../../lib/queries'

type StepStatus = 'pending' | 'active' | 'done' | 'failed'
type EventTone = 'info' | 'success' | 'danger'

interface FlowEvent {
  readonly id: number
  readonly time: string
  readonly tone: EventTone
  readonly text: string
}

const POLL_INTERVAL_MS = 3_000
const MAX_EVENTS = 8

const formatTime = () => new Date().toLocaleTimeString(undefined, { hour12: false })

function stepStatuses(state: string | undefined, isStarting: boolean, beginFailed: boolean, finishFailed: boolean): readonly StepStatus[] {
  if (isStarting) return ['active', 'pending', 'pending', 'pending']
  switch (state) {
    case 'waiting': return ['done', 'active', 'pending', 'pending']
    case 'scanned': return ['done', 'done', 'active', 'pending']
    case 'confirmed': return ['done', 'done', 'done', finishFailed ? 'failed' : 'active']
    case 'completed': return ['done', 'done', 'done', 'done']
    case 'expired': return ['done', 'failed', 'pending', 'pending']
    default: return [beginFailed ? 'failed' : 'pending', 'pending', 'pending', 'pending']
  }
}

export function LoginPage({ messages }: { readonly messages: MessageCatalog }) {
  const session = useSessionStatus()
  const switchableAccounts = useSwitchableAccounts(session.data?.state === 'authenticated')
  const mutations = useWorkspaceMutations()
  const [loginSessionId, setLoginSessionId] = useState('')
  const [qrCode, setQRCode] = useState<string>()
  const [loginState, setLoginState] = useState<string>()
  const [error, setError] = useState<string>()
  const [notice, setNotice] = useState<string>()
  const [confirmingLogout, setConfirmingLogout] = useState(false)
  const [events, setEvents] = useState<readonly FlowEvent[]>([])
  const [lastCheckedAt, setLastCheckedAt] = useState<string>()
  const [finishFailed, setFinishFailed] = useState(false)
  const eventIdRef = useRef(0)
  const lastErrorRef = useRef<string | undefined>(undefined)
  const autoCompleteRef = useRef(false)
  const refetchSession = session.refetch

  const pushEvent = useCallback((tone: EventTone, text: string) => {
    eventIdRef.current += 1
    const entry: FlowEvent = { id: eventIdRef.current, time: formatTime(), tone, text }
    setEvents((previous) => [entry, ...previous].slice(0, MAX_EVENTS))
  }, [])

  const reportError = useCallback((reason: unknown, fallback: string) => {
    const message = reason instanceof Error ? reason.message : fallback
    setError(message)
    if (lastErrorRef.current !== message) {
      lastErrorRef.current = message
      pushEvent('danger', message)
    }
  }, [pushEvent])

  const clearError = useCallback(() => {
    setError(undefined)
    lastErrorRef.current = undefined
  }, [])

  const poll = useCallback(() => mutations.pollLogin.mutate(undefined, {
    onSuccess: (result) => { clearError(); setLastCheckedAt(formatTime()); setLoginState(result.state) },
    onError: (reason) => { setLastCheckedAt(formatTime()); reportError(reason, messages.login.unavailable) }
  }), [clearError, messages.login.unavailable, mutations.pollLogin, reportError])
  const complete = useCallback(() => mutations.completeLogin.mutate(undefined, {
    onSuccess: () => { clearError(); setNotice(undefined); setFinishFailed(false); setLoginState('completed'); void refetchSession() },
    onError: (reason) => { setFinishFailed(true); reportError(reason, messages.login.unavailable) }
  }), [clearError, messages.login.unavailable, mutations.completeLogin, refetchSession, reportError])
  const active = loginState === 'waiting' || loginState === 'scanned'
  const readyToComplete = loginState === 'scanned' || loginState === 'confirmed'

  useEffect(() => {
    if (!loginSessionId || !qrCode || !active) return
    const timer = window.setInterval(() => {
      if (!mutations.pollLogin.isPending) poll()
    }, POLL_INTERVAL_MS)
    return () => window.clearInterval(timer)
  }, [active, loginSessionId, mutations.pollLogin.isPending, poll, qrCode])

  useEffect(() => {
    if (!loginState) return
    const catalog: Readonly<Record<string, readonly [EventTone, string]>> = {
      waiting: ['info', messages.login.statusWaiting],
      scanned: ['info', messages.login.statusScanned],
      confirmed: ['info', messages.login.statusConfirmed],
      completed: ['success', messages.login.statusCompleted],
      expired: ['danger', messages.login.statusExpired]
    }
    const entry = catalog[loginState]
    if (entry) pushEvent(entry[0], entry[1])
  }, [loginState, messages.login, pushEvent])

  useEffect(() => {
    if (loginState !== 'confirmed' || autoCompleteRef.current) return
    autoCompleteRef.current = true
    complete()
  }, [complete, loginState])

  const start = () => {
    clearError()
    setNotice(undefined); setQRCode(undefined); setLoginState(undefined)
    setFinishFailed(false); setEvents([]); setLastCheckedAt(undefined)
    autoCompleteRef.current = false
    pushEvent('info', messages.login.statusStarting)
    mutations.beginLogin.mutate(loginSessionId, {
      onSuccess: (flow) => { setLoginSessionId(flow.sessionId); setQRCode(flow.qrCode); setLoginState('waiting') },
      onError: (reason) => reportError(reason, messages.login.unavailable)
    })
  }
  const logout = () => {
    setConfirmingLogout(false)
    mutations.logout.mutate(undefined, {
      onSuccess: () => { clearError(); setNotice(messages.login.logoutComplete); setQRCode(undefined); setLoginState(undefined); setEvents([]); setLastCheckedAt(undefined); setFinishFailed(false) },
      onError: (reason) => reportError(reason, messages.login.logoutUnavailable)
    })
  }
  const switchIdentity = (id: string) => mutations.switchAccount.mutate(id, {
    onSuccess: (nextSession) => {
      clearError()
      setNotice(messages.login.switchComplete(nextSession.accountName?.trim() || messages.login.accountUnavailable))
    },
    onError: (reason) => reportError(reason, messages.login.switchUnavailable)
  })

  const isStarting = mutations.beginLogin.isPending
  const collapsed = session.data?.state === 'authenticated' && !loginState && !isStarting && !mutations.beginLogin.isError
  const statuses = stepStatuses(loginState, isStarting, mutations.beginLogin.isError, finishFailed)
  const steps = [
    { key: 'generate', label: messages.login.stepGenerate },
    { key: 'scan', label: messages.login.stepScan },
    { key: 'confirm', label: messages.login.stepConfirm },
    { key: 'finish', label: messages.login.stepFinish }
  ].map((step, index) => ({ ...step, status: statuses[index] }))

  const stateLabel = loginState ? messages.login.states[loginState] ?? messages.login.unknownState : undefined
  const chipTone: EventTone = loginState === 'completed' ? 'success' : loginState === 'expired' || finishFailed ? 'danger' : 'info'
  const statusMessage = (() => {
    if (isStarting) return messages.login.statusStarting
    if (finishFailed) return messages.login.statusFailed
    switch (loginState) {
      case 'waiting': return messages.login.statusWaiting
      case 'scanned': return messages.login.statusScanned
      case 'confirmed': return messages.login.statusConfirmed
      case 'completed': return messages.login.statusCompleted
      case 'expired': return messages.login.statusExpired
      default: return messages.login.statusIdle
    }
  })()

  return (
    <PageStack aria-labelledby="login-title">
      <PageHeader eyebrow={messages.navigation.system} title={messages.login.title} titleId="login-title" description={messages.login.description} supportingCopy={messages.login.legacyDescription} />
      <div className="login-layout">
        <Panel className="login-flow" aria-labelledby="qr-login-title">
          <h2 id="qr-login-title">{messages.login.qrTitle}</h2>
          <p>{collapsed ? messages.login.qrSignedIn : messages.login.qrDescription}</p>
          {collapsed ? null : (
          <div className="login-flow-body">
            <div className="qr-frame" data-qr-state={loginState === 'expired' || loginState === 'completed' ? loginState : undefined}>
              {qrCode
                ? <img className="qr-code" src={`data:image/png;base64,${qrCode}`} alt={messages.login.qrTitle} width="256" height="256" />
                : <div className="qr-placeholder" aria-hidden="true"><span>{messages.login.qrTitle}</span></div>}
              {qrCode && loginState === 'expired' ? <div className="qr-overlay" data-tone="danger">{messages.login.states.expired}</div> : null}
              {qrCode && loginState === 'completed' ? <div className="qr-overlay" data-tone="success">{messages.login.states.completed}</div> : null}
            </div>
            <ol className="login-steps" aria-label={messages.login.progressTitle}>
              {steps.map((step, index) => (
                <li key={step.key} className="login-step" data-step-status={step.status} aria-current={step.status === 'active' ? 'step' : undefined}>
                  <span className="login-step-marker" aria-hidden="true">{step.status === 'done' ? '✓' : step.status === 'failed' ? '✕' : index + 1}</span>
                  <span>{step.label}</span>
                </li>
              ))}
            </ol>
          </div>
          )}
          {collapsed ? null : (
          <div className="login-status-board">
            <div className="login-status-line" role="status" aria-live="polite">
              {stateLabel ? <span className="login-state-chip" data-tone={chipTone}>{stateLabel}</span> : null}
              <span className="login-status-message">{statusMessage}</span>
            </div>
            {active || lastCheckedAt ? (
              <p className="login-status-meta">
                {active ? messages.login.autoRefresh : null}
                {active && lastCheckedAt ? ' ' : null}
                {lastCheckedAt ? messages.login.lastChecked(lastCheckedAt) : null}
              </p>
            ) : null}
            {events.length > 0 ? (
              <section className="login-log" aria-label={messages.login.logTitle}>
                <h3>{messages.login.logTitle}</h3>
                <ol>
                  {events.map((event) => (
                    <li key={event.id} data-tone={event.tone}>
                      <span className="login-log-time">{event.time}</span>
                      <span>{event.text}</span>
                    </li>
                  ))}
                </ol>
              </section>
            ) : null}
          </div>
          )}
          {notice ? <p role="status">{notice}</p> : null}
          {error ? <p role="alert" className="login-error">{error}</p> : null}
          <ActionGroup align="start" gap="cluster">
            <Button label={messages.login.start} variant="primary" isLoading={mutations.beginLogin.isPending} isDisabled={active || mutations.pollLogin.isPending || mutations.completeLogin.isPending} onClick={start} />
            {collapsed ? null : <Button label={messages.login.poll} variant="ghost" size="sm" isDisabled={!active || mutations.pollLogin.isPending || mutations.completeLogin.isPending} onClick={poll} />}
            {collapsed ? null : <Button label={messages.login.complete} variant="ghost" size="sm" isDisabled={!readyToComplete || mutations.completeLogin.isPending} onClick={complete} />}
          </ActionGroup>
        </Panel>
        <div className="login-side">
          <Panel aria-labelledby="session-status-title">
            <h2 id="session-status-title">{messages.login.sessionTitle}</h2>
            {session.isLoading ? <p role="status">{messages.login.checking}</p> : null}
            {session.isError ? <ActionGroup align="start" gap="cluster" stackAt="compact"><p role="alert">{messages.login.unavailable}</p><Button label={messages.login.retry} variant="secondary" isLoading={session.isFetching} onClick={() => { void session.refetch() }} /></ActionGroup> : null}
            {session.data ? (
              <DefinitionList
                labelWidth="5.5rem"
                items={[
                  { term: messages.login.account, description: session.data.accountName?.trim() || (session.data.accountId ? messages.login.accountUnavailable : '—') },
                  { term: messages.login.state, description: messages.login.states[session.data.state] ?? messages.login.unknownState }
                ]}
              />
            ) : null}
            <p>{messages.login.manageGlobally}</p>
            {session.data?.state === 'authenticated' ? <ActionGroup align="start" gap="cluster" stackAt="compact"><Button label={messages.login.logout} variant="destructive" isLoading={mutations.logout.isPending} onClick={() => setConfirmingLogout(true)} /></ActionGroup> : null}
          </Panel>
          {session.data?.state === 'authenticated' ? (
            <Panel aria-labelledby="account-switch-title">
              <h2 id="account-switch-title">{messages.login.switchTitle}</h2>
              {switchableAccounts.isLoading ? <p role="status">{messages.login.switchChecking}</p> : null}
              {switchableAccounts.isError ? <p role="alert">{messages.login.switchUnavailable}</p> : null}
              {switchableAccounts.data && !switchableAccounts.data.available ? <p role="status">{messages.login.switchUnavailable}</p> : null}
              {switchableAccounts.data?.available && switchableAccounts.data.accounts.length === 0 ? <p>{messages.login.switchEmpty}</p> : null}
              {switchableAccounts.data?.available && switchableAccounts.data.accounts.length > 0 ? (
                <ActionGroup align="start" gap="cluster" stackAt="compact" aria-live="polite">
                  <Selector label={messages.login.switchAccount} options={switchableAccounts.data.accounts.map((account) => ({ value: account.id, label: account.name.trim() || account.alias?.trim() || messages.login.accountUnavailable, description: account.alias?.trim() || undefined }))} value={session.data?.accountId ?? ''} onChange={(next) => { if (next) switchIdentity(next) }} isDisabled={mutations.switchAccount.isPending} />
                  {mutations.switchAccount.isPending ? <p role="status">{messages.login.switching}</p> : null}
                </ActionGroup>
              ) : null}
            </Panel>
          ) : null}
        </div>
      </div>
      <ConfirmDialog
        isOpen={confirmingLogout}
        onOpenChange={(isOpen) => { if (!isOpen) setConfirmingLogout(false) }}
        title={messages.login.logoutConfirmTitle}
        description={messages.login.logoutConfirmDescription}
        closeLabel={messages.a11y.closeDialog}
        cancelLabel={messages.login.logoutConfirmCancel}
        actionLabel={messages.login.logout}
        onAction={logout}
      />
    </PageStack>
  )
}
