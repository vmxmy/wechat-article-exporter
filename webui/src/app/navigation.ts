export const navigationEvent = 'wechat-article:navigate'

export function navigateTo(href: string) {
  if (!href.startsWith('/') || href.startsWith('//')) return
  if (window.location.pathname === href) return

  window.history.pushState({}, '', href)
  window.dispatchEvent(new Event(navigationEvent))
}
