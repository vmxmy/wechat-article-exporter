import { Icons } from '@/components/icons'
import { cn } from '@/lib/utils'
import { useEffect, useId, useRef } from 'react'
import type { ReactNode, RefObject } from 'react'
import './presentation.css'

const focusableSelector = 'button:not([disabled]):not([tabindex="-1"]), [href]:not([tabindex="-1"]), input:not([disabled]):not([tabindex="-1"]), select:not([disabled]):not([tabindex="-1"]), textarea:not([disabled]):not([tabindex="-1"]), [tabindex]:not([tabindex="-1"])'

const isolatedElements = new Map<HTMLElement, { readonly ariaHidden: string | null; readonly inert: boolean }>()

function restoreModalIsolation() {
  for (const [element, state] of isolatedElements) {
    element.inert = state.inert
    if (state.ariaHidden === null) element.removeAttribute('aria-hidden')
    else element.setAttribute('aria-hidden', state.ariaHidden)
  }
  isolatedElements.clear()
}

function updateModalIsolation() {
  restoreModalIsolation()
  const overlays = Array.from(document.querySelectorAll<HTMLElement>('[data-presentation-drawer-overlay]'))
  let current = overlays.at(-1)
  while (current?.parentElement) {
    const parent = current.parentElement
    for (const sibling of parent.children) {
      if (sibling === current || !(sibling instanceof HTMLElement)) continue
      isolatedElements.set(sibling, { ariaHidden: sibling.getAttribute('aria-hidden'), inert: sibling.inert })
      sibling.inert = true
      sibling.setAttribute('aria-hidden', 'true')
    }
    if (parent === document.body) break
    current = parent
  }
}

export interface PresentationDrawerProps {
  readonly isOpen: boolean
  readonly onOpenChange: (isOpen: boolean) => void
  readonly title: string
  readonly description?: string
  readonly ariaLabel?: string
  readonly role?: 'dialog' | 'alertdialog'
  readonly closeLabel: string
  readonly children: ReactNode
  readonly footer?: ReactNode
  readonly width?: number | string
  readonly initialFocusRef?: RefObject<HTMLElement | null>
  readonly restoreFocusRef?: RefObject<HTMLElement | null>
  readonly restoreFocus?: boolean
  readonly className?: string
  readonly bodyClassName?: string
}

export function PresentationDrawer({
  isOpen,
  onOpenChange,
  title,
  description,
  ariaLabel,
  role = 'dialog',
  closeLabel,
  children,
  footer,
  width = 'min(36rem, 100vw)',
  initialFocusRef,
  restoreFocusRef,
  restoreFocus = true,
  className,
  bodyClassName
}: PresentationDrawerProps) {
  const overlayRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const onOpenChangeRef = useRef(onOpenChange)
  const titleId = useId()
  const descriptionId = useId()

  useEffect(() => {
    onOpenChangeRef.current = onOpenChange
  }, [onOpenChange])

  useEffect(() => {
    if (!isOpen) return
    const previousActiveElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const requestedRestoreTarget = restoreFocusRef?.current ?? null
    const isTopmost = () => {
      const overlays = document.querySelectorAll<HTMLElement>('[data-presentation-drawer-overlay]')
      return overlayRef.current !== null && overlays.item(overlays.length - 1) === overlayRef.current
    }
    updateModalIsolation()
    const focusInitialElement = () => {
      const initialFocus = initialFocusRef?.current
      if (initialFocus && !initialFocus.hasAttribute('disabled')) {
        initialFocus.focus()
        return
      }
      const panel = panelRef.current
      const requestedFocus = panel?.querySelector<HTMLElement>('[data-presentation-drawer-initial-focus]:not([disabled])')
      const autoFocus = panel?.querySelector<HTMLElement>('[autofocus]:not([disabled])')
      const firstFocusable = panel?.querySelector<HTMLElement>(focusableSelector)
      ;(requestedFocus ?? autoFocus ?? firstFocusable ?? panel)?.focus()
    }
    const animationFrame = window.requestAnimationFrame(focusInitialElement)
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isTopmost()) return
      if (event.key === 'Escape') {
        event.preventDefault()
        onOpenChangeRef.current(false)
        return
      }
      if (event.key !== 'Tab') return
      const panel = panelRef.current
      if (!panel) return
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>(focusableSelector))
      if (focusable.length === 0) {
        event.preventDefault()
        panel.focus()
        return
      }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    const onFocusIn = (event: FocusEvent) => {
      if (!isTopmost()) return
      const panel = panelRef.current
      if (panel && event.target instanceof Node && !panel.contains(event.target)) focusInitialElement()
    }
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('focusin', onFocusIn)
    return () => {
      window.cancelAnimationFrame(animationFrame)
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('focusin', onFocusIn)
      window.requestAnimationFrame(updateModalIsolation)
      if (!restoreFocus) return
      window.requestAnimationFrame(() => {
        const restoreTarget = requestedRestoreTarget ?? previousActiveElement
        if (restoreTarget?.isConnected) restoreTarget.focus()
      })
    }
  }, [initialFocusRef, isOpen, restoreFocus, restoreFocusRef])

  if (!isOpen) return null

  const close = () => onOpenChange(false)
  const onOverlayClick = () => {
    const overlays = document.querySelectorAll<HTMLElement>('[data-presentation-drawer-overlay]')
    if (overlays.item(overlays.length - 1) === overlayRef.current) close()
  }

  return (
    <div ref={overlayRef} className="presentation-drawer-overlay" data-presentation-drawer-overlay onClick={onOverlayClick}>
      <div
        ref={panelRef}
        role={role}
        aria-modal="true"
        aria-label={ariaLabel}
        aria-labelledby={ariaLabel ? undefined : titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
        className={cn('presentation-drawer-panel', className)}
        style={{ maxWidth: typeof width === 'number' ? `${width}px` : width }}
        onClick={(event) => event.stopPropagation()}
      >
        <header className="presentation-dialog-header">
          <div>
            <h2 id={titleId}>{title}</h2>
            {description ? <p id={descriptionId} className="presentation-dialog-description">{description}</p> : null}
          </div>
          <button className="presentation-dialog-close" type="button" aria-label={closeLabel} onClick={close}>
            <Icons.close aria-hidden="true" />
          </button>
        </header>
        <div className={cn('presentation-drawer-body', bodyClassName)}>{children}</div>
        {footer ? <footer className="presentation-dialog-footer">{footer}</footer> : null}
      </div>
    </div>
  )
}
