import { ConfirmDialog } from '@/components/controls/ConfirmDialog'
import { Button } from '@/components/controls/Button'
import { Selector } from '@/components/controls/Selector'
import { useCallback, useEffect, useState } from 'react'
import { ActionGroup, DefinitionList, PageHeader, PageStack, Panel } from '../../components/presentation'
import type { MessageCatalog } from '../../i18n'
import { useSessionStatus, useSwitchableAccounts, useWorkspaceMutations } from '../../lib/queries'

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

  const poll = useCallback(() => mutations.pollLogin.mutate(undefined, {
    onSuccess: (result) => { setError(undefined); setLoginState(result.state) },
    onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.unavailable)
  }), [messages.login.unavailable, mutations.pollLogin])
  const active = loginState === 'waiting' || loginState === 'scanned'
  const readyToComplete = loginState === 'scanned' || loginState === 'confirmed'

  useEffect(() => {
    if (!loginSessionId || !qrCode || !active) return
    const timer = window.setInterval(() => {
      if (!mutations.pollLogin.isPending) poll()
    }, 3_000)
    return () => window.clearInterval(timer)
  }, [active, loginSessionId, mutations.pollLogin.isPending, poll, qrCode])

  const start = () => {
    setError(undefined); setNotice(undefined); setQRCode(undefined); setLoginState(undefined)
    mutations.beginLogin.mutate(loginSessionId, {
    onSuccess: (flow) => { setLoginSessionId(flow.sessionId); setQRCode(flow.qrCode); setLoginState('waiting') },
      onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.unavailable)
    })
  }
  const complete = () => mutations.completeLogin.mutate(undefined, {
    onSuccess: () => { setError(undefined); setNotice(undefined); setLoginState('completed'); void session.refetch() },
    onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.unavailable)
  })
  const logout = () => {
    setConfirmingLogout(false)
    mutations.logout.mutate(undefined, {
    onSuccess: () => { setError(undefined); setNotice(messages.login.logoutComplete); setQRCode(undefined); setLoginState(undefined) },
      onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.logoutUnavailable)
    })
  }
  const switchIdentity = (id: string) => mutations.switchAccount.mutate(id, {
    onSuccess: (nextSession) => {
      setError(undefined)
      setNotice(messages.login.switchComplete(nextSession.accountName?.trim() || messages.login.accountUnavailable))
    },
    onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.switchUnavailable)
  })
  return (
    <PageStack aria-labelledby="login-title">
      <PageHeader eyebrow={messages.navigation.system} title={messages.login.title} titleId="login-title" description={messages.login.description} supportingCopy={messages.login.legacyDescription} />
      <div className="overview-grid login-grid">
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
        <Panel aria-labelledby="account-switch-title">
          <h2 id="account-switch-title">{messages.login.switchTitle}</h2>
          {session.data?.state === 'authenticated' && switchableAccounts.isLoading ? <p role="status">{messages.login.switchChecking}</p> : null}
          {session.data?.state === 'authenticated' && switchableAccounts.isError ? <p role="alert">{messages.login.switchUnavailable}</p> : null}
          {session.data?.state === 'authenticated' && switchableAccounts.data && !switchableAccounts.data.available ? <p role="status">{messages.login.switchUnavailable}</p> : null}
          {session.data?.state === 'authenticated' && switchableAccounts.data?.available && switchableAccounts.data.accounts.length === 0 ? <p>{messages.login.switchEmpty}</p> : null}
          {session.data?.state === 'authenticated' && switchableAccounts.data?.available && switchableAccounts.data.accounts.length > 0 ? (
            <ActionGroup align="start" gap="cluster" stackAt="compact" aria-live="polite">
              <Selector label={messages.login.switchAccount} options={switchableAccounts.data.accounts.map((account) => ({ value: account.id, label: account.name.trim() || account.alias?.trim() || messages.login.accountUnavailable, description: account.alias?.trim() || undefined }))} value={session.data?.accountId ?? ''} onChange={(next) => { if (next) switchIdentity(next) }} isDisabled={mutations.switchAccount.isPending} />
              {mutations.switchAccount.isPending ? <p role="status">{messages.login.switching}</p> : null}
            </ActionGroup>
          ) : null}
        </Panel>
        <Panel className="login-flow" aria-labelledby="qr-login-title">
          <h2 id="qr-login-title">{messages.login.qrTitle}</h2>
          <p>{messages.login.qrDescription}</p>
          {qrCode ? <img className="qr-code" src={`data:image/png;base64,${qrCode}`} alt={messages.login.qrTitle} width="256" height="256" /> : null}
          {loginState ? <p role="status">{messages.login.states[loginState] ?? messages.login.unknownState}</p> : null}
          {notice ? <p role="status">{notice}</p> : null}
          {error ? <p role="alert">{error}</p> : null}
          <ActionGroup align="start" gap="cluster" stackAt="compact">
            <Button label={messages.login.start} variant="primary" isLoading={mutations.beginLogin.isPending} isDisabled={active || mutations.pollLogin.isPending || mutations.completeLogin.isPending} onClick={start} />
            <Button label={messages.login.poll} variant="secondary" isDisabled={!active || mutations.pollLogin.isPending || mutations.completeLogin.isPending} onClick={poll} />
            <Button label={messages.login.complete} variant="secondary" isDisabled={!readyToComplete || mutations.completeLogin.isPending} onClick={complete} />
          </ActionGroup>
        </Panel>
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
