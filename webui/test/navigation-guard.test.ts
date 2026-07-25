import { describe, expect, it, vi } from 'vitest'
import type { Preferences } from '../src/lib/api'
import {
  NavigationGuardController,
  isPreferencesDirty,
  reconcileLoadedPreferences,
  reconcileSavedPreferences
} from '../src/lib/navigationGuard'

function preferences(overrides: Partial<Preferences> = {}): Preferences {
  return {
    sync: { range: 'all', pageDelay: 1, jitter: 0, pageSize: 20, incremental: true, unsafePacingSaved: false },
    download: { concurrency: 2, forceContent: false, metadataOverridesContent: false },
    export: {
      namingTemplate: '{{title}}',
      maximumNameBytes: 120,
      collisionPolicy: 'fail',
      excelIncludeContent: false,
      jsonIncludeContent: false,
      jsonIncludeComments: false,
      htmlIncludeComments: false
    },
    display: { noColor: false, ascii: false, plain: false, hideDeleted: false, language: 'en' },
    proxy: { directFirst: true, fallbackEnabled: false },
    ...overrides
  }
}

describe('settings unsaved navigation guard', () => {
  it('compares normalized editable preferences and clears dirty after a full reversion', () => {
    const baseline = preferences()
    const changed = preferences({ download: { ...baseline.download, concurrency: 5 } })
    const reverted = preferences({ download: { ...changed.download, concurrency: 2 } })
    const serverOnlyChange = preferences({ sync: { ...baseline.sync, pageSize: 50 } })

    expect(isPreferencesDirty(changed, baseline)).toBe(true)
    expect(isPreferencesDirty(reverted, baseline)).toBe(false)
    expect(isPreferencesDirty(serverOnlyChange, baseline)).toBe(false)
  })

  it('does not overwrite an active dirty draft when refreshed preferences arrive', () => {
    const baseline = preferences()
    const draft = preferences({ proxy: { directFirst: false, fallbackEnabled: false } })
    const refreshed = preferences({ download: { ...baseline.download, concurrency: 8 } })

    expect(reconcileLoadedPreferences(draft, baseline, refreshed)).toEqual({ draft, baseline })
    expect(reconcileLoadedPreferences(baseline, baseline, refreshed)).toEqual({ draft: refreshed, baseline: refreshed })
  })

  it('uses the successfully saved response as the new baseline without erasing edits made during save', () => {
    const baseline = preferences()
    const submitted = preferences({ download: { ...baseline.download, concurrency: 4 } })
    const saved = preferences({ download: { ...baseline.download, concurrency: 4 } })
    const editedDuringSave = preferences({ download: { ...baseline.download, concurrency: 6 } })

    expect(reconcileSavedPreferences(submitted, submitted, saved)).toEqual({ draft: saved, baseline: saved })
    expect(reconcileSavedPreferences(editedDuringSave, submitted, saved)).toEqual({ draft: editedDuringSave, baseline: saved })
    expect(isPreferencesDirty(editedDuringSave, saved)).toBe(true)
  })

  it('keeps the first blocked target, stays without resuming, and discards to resume it exactly once', () => {
    const guard = new NavigationGuardController()
    const firstTarget = vi.fn()
    const secondTarget = vi.fn()
    guard.setBlocker(() => true)

    expect(guard.request(firstTarget)).toBe(false)
    expect(guard.request(secondTarget)).toBe(false)
    expect(guard.hasPendingNavigation()).toBe(true)

    guard.stay()
    expect(firstTarget).not.toHaveBeenCalled()
    expect(secondTarget).not.toHaveBeenCalled()

    expect(guard.request(firstTarget)).toBe(false)
    guard.discard()
    guard.discard()

    expect(firstTarget).toHaveBeenCalledTimes(1)
    expect(guard.hasPendingNavigation()).toBe(false)
  })
})
