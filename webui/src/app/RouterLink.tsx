import type { AnchorHTMLAttributes, MouseEvent, PropsWithChildren } from 'react'
import { getClientNavigationHref, navigateTo } from './navigation'

export function RouterLink({ children, href, onClick, ...props }: PropsWithChildren<AnchorHTMLAttributes<HTMLAnchorElement>>) {
  function navigate(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event)
    const target = getClientNavigationHref(event.currentTarget, event.nativeEvent)
    if (!target) return

    event.preventDefault()
    navigateTo(target)
  }

  return <a {...props} href={href} data-wechat-router-link="true" onClick={navigate}>{children}</a>
}
