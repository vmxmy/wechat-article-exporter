import { DropdownMenu, type DropdownMenuItem } from '@/components/controls/DropdownMenu'
import { useState } from 'react'
import type { MessageCatalog } from '../i18n'
import { useSessionStatus, useSwitchableAccounts, useWorkspaceMutations } from '../lib/queries'
import { useWorkspaceSessionExpired } from '../lib/workspaceSession'
import { navigateTo } from './navigation'

export function SessionControl({ messages }: { readonly messages: MessageCatalog }) {
  const session = useSessionStatus()
  const workspaceExpired = useWorkspaceSessionExpired()
  const switchableAccounts = useSwitchableAccounts(session.data?.state === 'authenticated' && !workspaceExpired)
  const mutations = useWorkspaceMutations()
  const [notice, setNotice] = useState<string>()
  const [error, setError] = useState<string>()
  // React Query keeps the last successful payload when a refetch fails, so
  // without this the badge would keep advertising a WeChat account while the
  // workspace session behind every request is gone.
  const authenticated = !workspaceExpired && session.data?.state === 'authenticated'
  const accountLabel = workspaceExpired
    ? messages.login.workspaceExpiredBadge
    : session.data?.accountName?.trim() || (session.data?.accountId ? messages.login.accountUnavailable : messages.login.signedOut)

  const switchAccount = (id: string, name: string) => {
    mutations.switchAccount.mutate(id, {
      onSuccess: (nextSession) => {
        setError(undefined)
        setNotice(messages.login.switchComplete(nextSession.accountName?.trim() || name))
      },
      onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.switchUnavailable)
    })
  }

  const logout = () => {
    mutations.logout.mutate(undefined, {
      onSuccess: () => {
        setError(undefined)
        setNotice(messages.login.logoutComplete)
      },
      onError: (reason) => setError(reason instanceof Error ? reason.message : messages.login.logoutUnavailable)
    })
  }

  const accountItems = switchableAccounts.data?.available
    ? switchableAccounts.data.accounts
        .filter((account) => account.id !== session.data?.accountId)
        .map((account) => ({
          label: account.name.trim() || account.alias?.trim() || messages.login.accountUnavailable,
          onClick: () => switchAccount(account.id, account.name.trim() || account.alias?.trim() || messages.login.accountUnavailable),
          isDisabled: mutations.switchAccount.isPending
        }))
    : []

  const items: DropdownMenuItem[] = authenticated
    ? [
        { label: messages.login.manageSession, onClick: () => navigateTo('/login') },
        ...(accountItems.length > 0 ? [{ type: 'section' as const, title: messages.login.switchTitle, items: accountItems }] : []),
        { type: 'divider' },
        { label: messages.login.logout, onClick: logout, isDisabled: mutations.logout.isPending }
      ]
    : [{ label: messages.login.manageSession, onClick: () => navigateTo('/login') }]

  return (
    <div>
      <DropdownMenu
        button={{
          label: session.isLoading ? messages.login.checking : accountLabel,
          variant: 'secondary',
          size: 'sm',
          isDisabled: session.isLoading || session.isError
        }}
        items={items}
        menuWidth="min(22rem, calc(100vw - 2rem))"
      />
      {notice ? <span className="sr-only" role="status" aria-live="polite">{notice}</span> : null}
      {error ? <span className="sr-only" role="alert">{error}</span> : null}
    </div>
  )
}
