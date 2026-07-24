import { describe, expect, it } from 'vitest'
import {
  EMPTY_VALUE,
  formatBytes,
  formatCount,
  formatDateTime,
  formatDuration,
  formatEmpty,
  formatHash,
  formatJobKind,
  formatPath,
  formatShortIdentifier,
  formatStatus
} from '../src/lib/presentation/formatters'

describe('presentation formatters', () => {
  it('distinguishes missing values from known zero values', () => {
    expect(formatEmpty(undefined)).toBe(EMPTY_VALUE)
    expect(formatCount(undefined, 'en')).toBe(EMPTY_VALUE)
    expect(formatCount(0, 'en')).toBe('0')
    expect(formatBytes(0, 'en')).toBe('0 B')
    expect(formatDuration(0, 'en')).toContain('0')
  })

  it('formats numbers and bytes for the active locale', () => {
    expect(formatCount(1234567, 'en')).toBe('1,234,567')
    expect(formatCount(1234567, 'zh-CN')).toBe('1,234,567')
    expect(formatBytes(1536000, 'en')).toBe('1.54 MB')
  })

  it('formats dates and rejects invalid temporal values', () => {
    const options = { timeZone: 'UTC', dateStyle: 'medium', timeStyle: 'short' } as const
    expect(formatDateTime('2026-07-24T10:00:00Z', 'en', options)).toContain('2026')
    expect(formatDateTime('not-a-date', 'en')).toBe(EMPTY_VALUE)
  })

  it('formats durations with human units', () => {
    expect(formatDuration(45_000, 'en')).toBe('45 seconds')
    expect(formatDuration(90 * 60_000, 'en')).toBe('2 hours')
    expect(formatDuration(-1, 'en')).toBe(EMPTY_VALUE)
  })

  it('localizes supported statuses and job kinds without exposing snake case', () => {
    expect(formatStatus('blocked_auth', 'en')).toMatchObject({ label: 'Authentication required', tone: 'warning', isKnown: true })
    expect(formatStatus('blocked_auth', 'zh-CN').label).toBe('需要重新登录')
    expect(formatJobKind('article_download', 'en').label).toBe('Article download')
    expect(formatJobKind('album_sync', 'zh-CN').label).toBe('专辑同步')
  })

  it('hides unknown backend enum labels from normal presentation', () => {
    expect(formatStatus('future_backend_state', 'en')).toEqual({
      value: 'future_backend_state',
      label: EMPTY_VALUE,
      tone: 'neutral',
      isKnown: false
    })
    expect(formatStatus('future_backend_state', 'zh-CN').label).toBe(EMPTY_VALUE)
  })

  it('shortens technical values without changing their exact source', () => {
    expect(formatShortIdentifier('12345678-1234-1234-1234-1234567890ab')).toBe('123456…7890ab')
    expect(formatHash('0123456789abcdef0123456789abcdef')).toBe('01234567…89abcdef')
    expect(formatPath('/Users/example/Library/Application Support/Exporter/output/article.html', 32)).toMatch(/^\/Users\/example\/.*…\/article\.html$/)
  })
})
