import { Button } from '@/components/controls/Button'
import { Collapsible } from '@/components/controls/Collapsible'
import { useEffect, useRef, useState } from 'react'
import { EMPTY_VALUE } from '../../lib/presentation'
import './presentation.css'

export interface TechnicalDetailItem {
  readonly label: string
  readonly value: string | number | boolean | null | undefined
  readonly copyLabel: string
  readonly copiedLabel?: string
  readonly copyFailedLabel?: string
}

export interface TechnicalDetailsProps {
  readonly label: string
  readonly items: readonly TechnicalDetailItem[]
  readonly defaultIsOpen?: boolean
  readonly onCopy?: (item: TechnicalDetailItem) => void
}

export function TechnicalDetails({ label, items, defaultIsOpen = false, onCopy }: TechnicalDetailsProps) {
  return (
    <Collapsible trigger={label} defaultIsOpen={defaultIsOpen}>
      <dl className="presentation-technical-list">
        {items.map((item) => <TechnicalDetail key={item.label} item={item} onCopy={onCopy} />)}
      </dl>
    </Collapsible>
  )
}

type CopyFeedback = 'idle' | 'copied' | 'failed'

function TechnicalDetail({ item, onCopy }: { readonly item: TechnicalDetailItem; readonly onCopy?: (item: TechnicalDetailItem) => void }) {
  const exactValue = exactString(item.value)
  const [feedback, setFeedback] = useState<CopyFeedback>('idle')
  const resetTimer = useRef<number | undefined>(undefined)
  const copiedLabel = item.copiedLabel ?? item.copyLabel

  useEffect(() => () => {
    if (resetTimer.current) window.clearTimeout(resetTimer.current)
  }, [])

  const copy = async () => {
    if (!exactValue) return
    if (!navigator.clipboard || typeof navigator.clipboard.writeText !== 'function') {
      announce('failed')
      return
    }
    try {
      await navigator.clipboard.writeText(exactValue)
      announce('copied')
      onCopy?.(item)
    } catch {
      announce('failed')
    }
  }

  const announce = (next: CopyFeedback) => {
    setFeedback(next)
    if (resetTimer.current) window.clearTimeout(resetTimer.current)
    resetTimer.current = window.setTimeout(() => setFeedback('idle'), 2_000)
  }

  const feedbackMessage = feedback === 'copied'
    ? item.copiedLabel
    : feedback === 'failed'
      ? item.copyFailedLabel
      : undefined

  return (
    <div className="presentation-technical-item">
      <dt className="presentation-technical-label">{item.label}</dt>
      <dd className="presentation-technical-value">
        <code className="presentation-code" translate="no">{exactValue || EMPTY_VALUE}</code>
        {exactValue ? (
          <Button
            label={feedback === 'copied' ? copiedLabel : item.copyLabel}
            variant="ghost"
            size="sm"
            onClick={copy}
          />
        ) : null}
        {feedbackMessage ? (
          <span id={`${item.label}-copy-status`} className="sr-only" role={feedback === 'failed' ? 'alert' : 'status'} aria-live={feedback === 'failed' ? 'assertive' : 'polite'}>{feedbackMessage}</span>
        ) : null}
      </dd>
    </div>
  )
}

function exactString(value: TechnicalDetailItem['value']): string {
  if (value === null || value === undefined) return ''
  return typeof value === 'string' ? value : String(value)
}
