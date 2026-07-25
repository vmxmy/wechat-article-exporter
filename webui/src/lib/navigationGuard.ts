import type { Preferences } from './api'

export type NavigationResume = () => void

type PreferencesSnapshot = {
  readonly download: Preferences['download']
  readonly export: Preferences['export']
  readonly display: Pick<Preferences['display'], 'language'>
  readonly proxy: Preferences['proxy']
}

export class NavigationGuardController {
  private blocker?: () => boolean
  private pending?: NavigationResume
  private listeners = new Set<() => void>()
  private bypass = false

  setBlocker(blocker: (() => boolean) | undefined) {
    this.blocker = blocker
    if (blocker && this.pending && !blocker()) {
      const resume = this.pending
      this.pending = undefined
      this.emit()
      resume()
    }
  }

  request(resume: NavigationResume): boolean {
    if (this.bypass || !this.blocker?.()) {
      resume()
      return true
    }
    if (!this.pending) this.pending = resume
    this.emit()
    return false
  }

  stay() {
    this.pending = undefined
    this.emit()
  }

  discard() {
    const resume = this.pending
    if (!resume) return
    this.pending = undefined
    this.emit()
    this.bypass = true
    try {
      resume()
    } finally {
      this.bypass = false
    }
  }

  hasPendingNavigation() {
    return Boolean(this.pending)
  }

  subscribe(listener: () => void) {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }

  private emit() {
    for (const listener of this.listeners) listener()
  }
}

export const navigationGuard = new NavigationGuardController()

export function editablePreferences(value: Preferences): PreferencesSnapshot {
  return {
    download: { ...value.download },
    export: { ...value.export },
    display: { language: value.display.language },
    proxy: { ...value.proxy }
  }
}

export function isPreferencesDirty(draft: Preferences, baseline: Preferences): boolean {
  return JSON.stringify(editablePreferences(draft)) !== JSON.stringify(editablePreferences(baseline))
}

export function reconcileLoadedPreferences(draft: Preferences, baseline: Preferences, loaded: Preferences) {
  return isPreferencesDirty(draft, baseline) ? { draft, baseline } : { draft: loaded, baseline: loaded }
}

export function reconcileSavedPreferences(draft: Preferences, submitted: Preferences, saved: Preferences) {
  return isPreferencesDirty(draft, submitted) ? { draft, baseline: saved } : { draft: saved, baseline: saved }
}
