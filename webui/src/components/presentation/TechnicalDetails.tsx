import { Button } from '@astryxdesign/core/Button'
import { Collapsible } from '@astryxdesign/core/Collapsible'
import { EMPTY_VALUE } from '../../lib/presentation'
import './presentation.css'

export interface TechnicalDetailItem {
  readonly label: string
  readonly value: string | number | boolean | null | undefined
  readonly copyLabel: string
  readonly copiedLabel?: string
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

function TechnicalDetail({ item, onCopy }: { readonly item: TechnicalDetailItem; readonly onCopy?: (item: TechnicalDetailItem) => void }) {
  const exactValue = exactString(item.value)
  const copy = async () => {
    if (!exactValue) return
    try {
      await navigator.clipboard?.writeText(exactValue)
      onCopy?.(item)
    } catch {
      // Copy is an enhancement for technical diagnostics. Browsers that deny
      // clipboard access must leave the exact value visible and usable.
    }
  }
  return (
    <div className="presentation-technical-item">
      <dt className="presentation-technical-label">{item.label}</dt>
      <dd className="presentation-technical-value">
        <code className="presentation-code">{exactValue || EMPTY_VALUE}</code>
        {exactValue ? <Button label={item.copyLabel} variant="ghost" size="sm" clickAction={copy} /> : null}
      </dd>
    </div>
  )
}

function exactString(value: TechnicalDetailItem['value']): string {
  if (value === null || value === undefined) return ''
  return typeof value === 'string' ? value : String(value)
}
