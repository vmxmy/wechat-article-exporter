import type { HTMLAttributes, ReactNode } from 'react'

type LayoutElement = 'div' | 'section'
type PageStackGap = 'section' | 'subsection'
type SectionStackGap = 'section' | 'subsection' | 'cluster'

type LayoutRhythmProps = HTMLAttributes<HTMLElement> & {
  readonly children: ReactNode
}

interface StackProps extends LayoutRhythmProps {
  readonly as?: LayoutElement
}

interface PageStackProps extends StackProps {
  readonly gap?: PageStackGap
}

interface SectionStackProps extends StackProps {
  readonly gap?: SectionStackGap
}

interface ContentClusterProps extends LayoutRhythmProps {
  readonly align?: 'start' | 'center' | 'end'
  readonly justify?: 'start' | 'between' | 'end'
}

interface ReadingMeasureProps extends LayoutRhythmProps {
  readonly as?: LayoutElement
  readonly size?: 'reading' | 'description' | 'form'
}

export function PageStack({ as = 'section', children, className, gap = 'section', ...props }: PageStackProps) {
  const classNameValue = classNames('layout-page-stack', `layout-page-stack--${gap}`, className)
  if (as === 'div') return <div {...props} className={classNameValue}>{children}</div>
  return <section {...props} className={classNameValue}>{children}</section>
}

export function SectionStack({ as = 'div', children, className, gap = 'cluster', ...props }: SectionStackProps) {
  const classNameValue = classNames('layout-section-stack', `layout-section-stack--${gap}`, className)
  if (as === 'section') return <section {...props} className={classNameValue}>{children}</section>
  return <div {...props} className={classNameValue}>{children}</div>
}

export function ContentCluster({ children, className, align = 'center', justify = 'start', ...props }: ContentClusterProps) {
  return <div {...props} className={classNames('layout-content-cluster', `layout-content-cluster--align-${align}`, `layout-content-cluster--justify-${justify}`, className)}>{children}</div>
}

export function ReadingMeasure({ as = 'div', children, className, size = 'reading', ...props }: ReadingMeasureProps) {
  const classNameValue = classNames('layout-reading-measure', `layout-reading-measure--${size}`, className)
  if (as === 'section') return <section {...props} className={classNameValue}>{children}</section>
  return <div {...props} className={classNameValue}>{children}</div>
}

export function DenseRegion({ children, className, ...props }: LayoutRhythmProps) {
  return <div {...props} className={classNames('layout-dense-region', className)}>{children}</div>
}

function classNames(...values: readonly (string | undefined)[]) {
  return values.filter(Boolean).join(' ')
}
