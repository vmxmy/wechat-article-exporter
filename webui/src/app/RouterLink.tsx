import type { AnchorHTMLAttributes, MouseEvent, PropsWithChildren } from 'react'
import { navigateTo } from './navigation'

export function RouterLink({ children, href, onClick, ...props }: PropsWithChildren<AnchorHTMLAttributes<HTMLAnchorElement>>) {
  function navigate(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event)
    if (
      event.defaultPrevented
      || !href?.startsWith('/')
      || href.startsWith('//')
      || event.button !== 0
      || event.metaKey
      || event.ctrlKey
      || event.shiftKey
      || event.altKey
      || (event.currentTarget.target && event.currentTarget.target !== '_self')
      || event.currentTarget.hasAttribute('download')
    ) {
      return
    }

    event.preventDefault()
    navigateTo(href)
  }

  return <a {...props} href={href} onClick={navigate}>{children}</a>
}
