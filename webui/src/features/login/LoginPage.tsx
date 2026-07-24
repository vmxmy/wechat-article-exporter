import { Button } from '@astryxdesign/core/Button'
import { useCallback, useEffect, useState } from 'react'
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
  const logout = () => mutations.logout.mutate(undefined, {
    onSuccess: () => { setError(undefined); setNotice(messages.login.logoutComplete); setQRCode(undefined); setLoginState(undefined) },
    onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.logoutUnavailable)
  })
  const switchIdentity = (id: string) => mutations.switchAccount.mutate(id, {
    onSuccess: (nextSession) => {
      setError(undefined)
      setNotice(messages.login.switchComplete(nextSession.accountName ?? id))
    },
    onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.switchUnavailable)
  })
  return (
    <section aria-labelledby="login-title">
      <header className="page-heading">
        <div>
          <p className="eyebrow">{messages.navigation.workspace}</p>
          <h1 id="login-title">{messages.login.title}</h1>
          <p className="lede">{messages.login.description}</p>
        </div>
      </header>
      <div className="overview-grid login-grid">
        <section className="workspace-panel" aria-labelledby="session-status-title">
          <h2 id="session-status-title">{messages.login.sessionTitle}</h2>
          {session.isLoading ? <p role="status">{messages.login.checking}</p> : null}
          {session.isError ? <p role="alert">{messages.login.unavailable}</p> : null}
          {session.data ? <dl className="facts-list"><div><dt>{messages.login.account}</dt><dd>{session.data.accountName ?? '—'}</dd></div><div><dt>{messages.login.state}</dt><dd>{messages.login.states[session.data.state] ?? session.data.state}</dd></div></dl> : null}
          {session.data?.state === 'authenticated' ? <div className="action-button-group"><Button label={messages.login.logout} variant="destructive" isLoading={mutations.logout.isPending} onClick={logout} /></div> : null}
        </section>
        <section className="workspace-panel" aria-labelledby="account-switch-title">
          <h2 id="account-switch-title">{messages.login.switchTitle}</h2>
          {session.data?.state === 'authenticated' && switchableAccounts.isLoading ? <p role="status">{messages.login.switchChecking}</p> : null}
          {session.data?.state === 'authenticated' && switchableAccounts.isError ? <p role="alert">{messages.login.switchUnavailable}</p> : null}
          {session.data?.state === 'authenticated' && switchableAccounts.data && !switchableAccounts.data.available ? <p role="status">{messages.login.switchUnavailable}</p> : null}
          {session.data?.state === 'authenticated' && switchableAccounts.data?.available && switchableAccounts.data.accounts.length === 0 ? <p>{messages.login.switchEmpty}</p> : null}
          {session.data?.state === 'authenticated' && switchableAccounts.data?.available && switchableAccounts.data.accounts.length > 0 ? (
            <div className="action-button-group" aria-live="polite">
              <label htmlFor="switchable-account">{messages.login.switchAccount}</label>
              <select id="switchable-account" value={session.data?.accountId ?? ''} onChange={(event) => switchIdentity(event.target.value)} disabled={mutations.switchAccount.isPending}>
                {switchableAccounts.data.accounts.map((account) => <option key={account.id} value={account.id}>{account.name || account.alias || account.id}</option>)}
              </select>
              {mutations.switchAccount.isPending ? <p role="status">{messages.login.switching}</p> : null}
            </div>
          ) : null}
        </section>
        <section className="workspace-panel login-flow" aria-labelledby="qr-login-title">
          <h2 id="qr-login-title">{messages.login.qrTitle}</h2>
          <p>{messages.login.qrDescription}</p>
          {qrCode ? <img className="qr-code" src={`data:image/png;base64,${qrCode}`} alt={messages.login.qrTitle} /> : null}
          {loginState ? <p role="status">{messages.login.states[loginState] ?? loginState}</p> : null}
          {notice ? <p role="status">{notice}</p> : null}
          {error ? <p role="alert">{error}</p> : null}
          <div className="action-button-group">
            <Button label={messages.login.start} variant="primary" isLoading={mutations.beginLogin.isPending} isDisabled={active || mutations.pollLogin.isPending || mutations.completeLogin.isPending} onClick={start} />
            <Button label={messages.login.poll} variant="secondary" isDisabled={!active || mutations.pollLogin.isPending || mutations.completeLogin.isPending} onClick={poll} />
            <Button label={messages.login.complete} variant="secondary" isDisabled={!readyToComplete || mutations.completeLogin.isPending} onClick={complete} />
          </div>
        </section>
      </div>
    </section>
  )
}
