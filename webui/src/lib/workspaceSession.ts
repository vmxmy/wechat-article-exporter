import { useSyncExternalStore } from 'react'

/**
 * Tracks the *local browser workspace* session, which is not the WeChat session.
 * The server answers both with 401, so it distinguishes them by error code:
 * `authentication_required` means this browser's workspace cookie is gone and no
 * amount of signing in to WeChat will fix it — the workspace has to be re-opened.
 * Without this signal the UI keeps rendering the last successful session payload
 * and looks signed in while every request fails.
 */
const workspaceSessionErrorCode = 'authentication_required'

const listeners = new Set<() => void>()
let expired = false

export function reportApiFailure(status: number, code: string): void {
  if (status !== 401 || code !== workspaceSessionErrorCode || expired) return
  expired = true
  for (const listener of listeners) listener()
}

export function isWorkspaceSessionExpired(): boolean {
  return expired
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function useWorkspaceSessionExpired(): boolean {
  return useSyncExternalStore(subscribe, isWorkspaceSessionExpired, () => false)
}
