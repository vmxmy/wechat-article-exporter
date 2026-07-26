import type { CSSProperties, ReactNode } from 'react'
import './presentation-layout.css'

export interface DefinitionListItem {
  readonly term: string
  readonly description: ReactNode
}

export interface DefinitionListProps {
  /** `rows` = label/value rows; `tiles` = auto-fit cards. */
  readonly layout?: 'rows' | 'tiles'
  /** `rows` only: minimum width of the term column (CSS length). */
  readonly labelWidth?: string
  /** `rows` only: collapse to a single stacked column below this breakpoint. */
  readonly collapseAt?: 'compact' | 'medium'
  /** Vertical rhythm between rows. `tight` = spacing-2 (default); `relaxed` = spacing-3. */
  readonly rowGap?: 'tight' | 'relaxed'
  readonly items: ReadonlyArray<DefinitionListItem>
}

export function DefinitionList({ layout = 'rows', labelWidth, collapseAt, rowGap = 'tight', items }: DefinitionListProps) {
  const style = labelWidth ? ({ '--definition-list-label': labelWidth } as CSSProperties) : undefined
  return (
    <dl
      className="presentation-definition-list"
      data-layout={layout}
      data-collapse-at={collapseAt}
      data-row-gap={rowGap}
      style={style}
    >
      {items.map((item) => (
        <div key={item.term}>
          <dt>{item.term}</dt>
          <dd>{item.description}</dd>
        </div>
      ))}
    </dl>
  )
}
