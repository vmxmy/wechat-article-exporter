export const EMPTY_VALUE = '—'

export type PresentationLocale = 'en' | 'zh-CN'
export type SemanticTone = 'success' | 'warning' | 'error' | 'accent' | 'neutral'

export interface LocalizedSemanticValue {
  readonly value: string
  readonly label: string
  readonly tone: SemanticTone
  readonly isKnown: boolean
}

type LocalizedLabel = Readonly<Record<PresentationLocale, string>>
type SemanticDefinition = {
  readonly label: LocalizedLabel
  readonly tone: SemanticTone
}

const statusDefinitions = {
  queued: definition('Queued', '等待中', 'neutral'),
  pending: definition('Pending', '待处理', 'neutral'),
  running: definition('Running', '进行中', 'accent'),
  completed: definition('Completed', '已完成', 'success'),
  succeeded: definition('Succeeded', '已成功', 'success'),
  success: definition('Successful', '成功', 'success'),
  partial: definition('Partially completed', '部分完成', 'warning'),
  failed: definition('Failed', '失败', 'error'),
  error: definition('Error', '错误', 'error'),
  cancelled: definition('Cancelled', '已取消', 'neutral'),
  canceled: definition('Cancelled', '已取消', 'neutral'),
  blocked_auth: definition('Authentication required', '需要重新登录', 'warning'),
  paused: definition('Paused', '已暂停', 'warning'),
  active: definition('Active', '启用', 'success'),
  enabled: definition('Enabled', '已启用', 'success'),
  valid: definition('Valid', '有效', 'success'),
  ready: definition('Ready', '就绪', 'success'),
  available: definition('Available', '可用', 'success'),
  inactive: definition('Inactive', '未启用', 'neutral'),
  disabled: definition('Disabled', '已停用', 'neutral'),
  invalid: definition('Invalid', '无效', 'error'),
  unavailable: definition('Unavailable', '不可用', 'error'),
  missing: definition('Missing', '缺失', 'warning'),
  unknown: definition('Unknown', '未知', 'neutral')
} as const satisfies Readonly<Record<string, SemanticDefinition>>

const jobKindDefinitions = {
  account_sync: definition('Account sync', '账号同步', 'accent'),
  album_sync: definition('Album sync', '专辑同步', 'accent'),
  article_download: definition('Article download', '文章下载', 'accent'),
  resource_download: definition('Resource download', '资源下载', 'accent'),
  metadata_download: definition('Metrics download', '数据下载', 'accent'),
  comments_download: definition('Comments download', '评论下载', 'accent'),
  paid_content_download: definition('Paid content download', '付费内容下载', 'accent'),
  export: definition('Export', '导出', 'accent'),
  download: definition('Download', '下载', 'accent'),
  sync: definition('Sync', '同步', 'accent'),
  route_probe: definition('Network check', '网络检查', 'neutral'),
  diagnostic: definition('Diagnostics', '诊断', 'neutral')
} as const satisfies Readonly<Record<string, SemanticDefinition>>

const byteUnits = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'] as const

export function formatEmpty(value: unknown, fallback = EMPTY_VALUE): string {
  if (value === null || value === undefined) return fallback
  if (typeof value === 'string' && value.trim() === '') return fallback
  return String(value)
}

export function formatStatus(value: string | null | undefined, locale: PresentationLocale): LocalizedSemanticValue {
  return formatSemanticValue(value, locale, statusDefinitions)
}

export function formatJobKind(value: string | null | undefined, locale: PresentationLocale): LocalizedSemanticValue {
  return formatSemanticValue(value, locale, jobKindDefinitions)
}

export function formatDateTime(
  value: Date | string | number | null | undefined,
  locale: PresentationLocale,
  options: Intl.DateTimeFormatOptions = { dateStyle: 'medium', timeStyle: 'short' }
): string {
  const date = toValidDate(value)
  return date ? new Intl.DateTimeFormat(locale, options).format(date) : EMPTY_VALUE
}

export function formatDate(
  value: Date | string | number | null | undefined,
  locale: PresentationLocale,
  options: Intl.DateTimeFormatOptions = { dateStyle: 'medium' }
): string {
  return formatDateTime(value, locale, options)
}

const relativeThresholds = [
  { unit: 'second', milliseconds: 1000, limit: 60 },
  { unit: 'minute', milliseconds: 60_000, limit: 60 },
  { unit: 'hour', milliseconds: 3_600_000, limit: 24 },
  { unit: 'day', milliseconds: 86_400_000, limit: 7 },
  { unit: 'week', milliseconds: 604_800_000, limit: 4.35 },
  { unit: 'month', milliseconds: 2_629_800_000, limit: 12 },
  { unit: 'year', milliseconds: 31_557_600_000, limit: Infinity }
] as const satisfies readonly { unit: Intl.RelativeTimeFormatUnit; milliseconds: number; limit: number }[]

/** `now` is injectable so callers can render against a shared clock tick and
    tests stay deterministic. */
export function formatRelativeTime(
  value: Date | string | number | null | undefined,
  locale: PresentationLocale,
  now: number = Date.now()
): string {
  const date = toValidDate(value)
  if (!date) return EMPTY_VALUE
  const elapsed = date.getTime() - now
  const magnitude = Math.abs(elapsed)
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  for (const threshold of relativeThresholds) {
    const scaled = magnitude / threshold.milliseconds
    if (scaled < threshold.limit) {
      const rounded = Math.round(elapsed / threshold.milliseconds)
      return formatter.format(rounded, threshold.unit)
    }
  }
  return formatter.format(Math.round(elapsed / 31_557_600_000), 'year')
}

export interface JobKindOption {
  readonly value: string
  readonly label: string
}

/** The filterable job kinds, ordered by their localized label. */
export function listJobKinds(locale: PresentationLocale): readonly JobKindOption[] {
  const collator = new Intl.Collator(locale)
  return Object.keys(jobKindDefinitions)
    .map((value) => ({ value, label: formatJobKind(value, locale).label }))
    .sort((first, second) => collator.compare(first.label, second.label))
}

export function formatDuration(milliseconds: number | null | undefined, locale: PresentationLocale): string {
  if (milliseconds === null || milliseconds === undefined || !Number.isFinite(milliseconds) || milliseconds < 0) return EMPTY_VALUE
  if (milliseconds === 0) return formatUnit(0, 'millisecond', locale)

  const totalSeconds = Math.round(milliseconds / 1000)
  if (totalSeconds < 60) return formatUnit(totalSeconds, 'second', locale)

  const totalMinutes = Math.round(totalSeconds / 60)
  if (totalMinutes < 60) return formatUnit(totalMinutes, 'minute', locale)

  const totalHours = Math.round(totalMinutes / 60)
  if (totalHours < 24) return formatUnit(totalHours, 'hour', locale)

  return formatUnit(Math.round(totalHours / 24), 'day', locale)
}

export function formatBytes(bytes: number | null | undefined, locale: PresentationLocale): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(bytes) || bytes < 0) return EMPTY_VALUE
  if (bytes === 0) return `0 ${byteUnits[0]}`

  const unitIndex = Math.min(Math.floor(Math.log(bytes) / Math.log(1000)), byteUnits.length - 1)
  const value = bytes / (1000 ** unitIndex)
  const maximumFractionDigits = value >= 100 || unitIndex === 0 ? 0 : value >= 10 ? 1 : 2
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits }).format(value)} ${byteUnits[unitIndex]}`
}

export function formatCount(value: number | null | undefined, locale: PresentationLocale): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return EMPTY_VALUE
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(value)
}

export function formatShortIdentifier(value: string | null | undefined, edgeLength = 6): string {
  const normalized = value?.trim()
  if (!normalized) return EMPTY_VALUE
  if (!Number.isInteger(edgeLength) || edgeLength < 2) return normalized
  if (normalized.length <= edgeLength * 2 + 1) return normalized
  return `${normalized.slice(0, edgeLength)}…${normalized.slice(-edgeLength)}`
}

export function formatPath(value: string | null | undefined, maximumLength = 44): string {
  const normalized = value?.trim()
  if (!normalized) return EMPTY_VALUE
  if (normalized.length <= maximumLength) return normalized

  const separator = normalized.includes('\\') && !normalized.includes('/') ? '\\' : '/'
  const parts = normalized.split(separator).filter(Boolean)
  const prefix = normalized.startsWith(separator) ? separator : ''
  const tail = parts.at(-1) ?? normalized
  if (tail.length + 2 >= maximumLength) return `…${tail.slice(-(maximumLength - 1))}`

  const remaining = maximumLength - tail.length - 2
  const head = `${prefix}${parts.slice(0, -1).join(separator)}`
  return `${head.slice(0, remaining)}…${separator}${tail}`
}

export function formatHash(value: string | null | undefined): string {
  return formatShortIdentifier(value, 8)
}

function definition(en: string, zhCN: string, tone: SemanticTone): SemanticDefinition {
  return { label: { en, 'zh-CN': zhCN }, tone }
}

function formatSemanticValue(
  value: string | null | undefined,
  locale: PresentationLocale,
  definitions: Readonly<Record<string, SemanticDefinition>>
): LocalizedSemanticValue {
  const normalized = value?.trim().toLowerCase()
  if (!normalized) return { value: '', label: EMPTY_VALUE, tone: 'neutral', isKnown: false }
  const known = definitions[normalized]
  if (known) return { value: normalized, label: known.label[locale], tone: known.tone, isKnown: true }
  return { value: normalized, label: EMPTY_VALUE, tone: 'neutral', isKnown: false }
}

function toValidDate(value: Date | string | number | null | undefined): Date | undefined {
  if (value === null || value === undefined || value === '') return undefined
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date
}

function formatUnit(value: number, unit: Intl.NumberFormatOptions['unit'], locale: PresentationLocale): string {
  return new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'long', maximumFractionDigits: 0 }).format(value)
}
