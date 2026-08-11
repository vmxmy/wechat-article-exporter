import type { ReactNode } from 'react'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

export interface TabNavigationItem {
  value: string
  label: string
}

export interface TabNavigationProps {
  label: string
  value: string
  onChange: (value: string) => void
  items: readonly TabNavigationItem[]
  children?: ReactNode
  className?: string
}

/**
 * Horizontal tab strip over Base UI, which supplies the tablist/tab/tabpanel roles, roving
 * tabindex, and arrow-key movement. Activation stays manual so arrowing through tabs does not
 * commit a selection, and panels stay unmounted while inactive.
 */
export function TabNavigation({ label, value, onChange, items, children, className }: TabNavigationProps) {
  return (
    <Tabs value={value} onValueChange={(next) => onChange(String(next))} className={cn('control-tabs', className)}>
      <TabsList aria-label={label} className="control-tab-list w-full max-w-full justify-start overflow-x-auto">
        {items.map((item) => (
          <TabsTrigger key={item.value} value={item.value} className="control-tab h-auto flex-none">{item.label}</TabsTrigger>
        ))}
      </TabsList>
      {children}
    </Tabs>
  )
}

export interface TabPanelProps {
  value: string
  children?: ReactNode
  className?: string
}

/** Base UI generates the panel id that its tab's `aria-controls` points at, so callers must not override it. */
export function TabPanel({ value, children, className }: TabPanelProps) {
  return <TabsContent value={value} className={cn('control-tab-panel min-w-0', className)}>{children}</TabsContent>
}
