import { StatusDot } from '@/components/controls/StatusDot'
import { formatStatus, type PresentationLocale, type SemanticTone } from '../../lib/presentation'
import './presentation.css'

export interface StatusProps {
  readonly value: string | null | undefined
  readonly locale: PresentationLocale
  readonly label?: string
  readonly tone?: SemanticTone
  readonly tooltip?: string
  readonly isPulsing?: boolean
}

export function Status({ value, locale, label, tone, tooltip, isPulsing = false }: StatusProps) {
  const formatted = formatStatus(value, locale)
  const visibleLabel = label ?? formatted.label
  return (
    <span className="presentation-status" data-status-value={formatted.isKnown ? formatted.value : undefined}>
      <StatusDot variant={tone ?? formatted.tone} label={visibleLabel} tooltip={tooltip} isPulsing={isPulsing} />
      <span>{visibleLabel}</span>
    </span>
  )
}
