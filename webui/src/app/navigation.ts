export const navigationEvent = 'wechat-article:navigate'

export const navigationGroups = ['home', 'content', 'work', 'system'] as const

export const navigationItems = [
  { group: 'home', href: '/', key: 'overview' },
  { group: 'content', href: '/accounts', key: 'accounts' },
  { group: 'content', href: '/articles', key: 'articles' },
  { group: 'content', href: '/albums', key: 'albums' },
  { group: 'work', href: '/jobs', key: 'jobs' },
  { group: 'work', href: '/exports', key: 'exports' },
  { group: 'work', href: '/import', key: 'import' },
  { group: 'system', href: '/settings', key: 'settings' }
] as const

export type NavigationGroup = typeof navigationGroups[number]
export type NavigationItem = typeof navigationItems[number]

export function getNavigationItem(path: string): NavigationItem {
  return navigationItems.find((item) => item.href === path) ?? navigationItems[0]
}

export function navigateTo(href: string) {
  if (!href.startsWith('/') || href.startsWith('//')) return
  if (window.location.pathname === href) return

  window.history.pushState({}, '', href)
  window.dispatchEvent(new Event(navigationEvent))
}
