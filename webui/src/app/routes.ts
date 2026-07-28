import { navigationGroups, navigationItems, type NavigationGroup, type NavigationItem } from './navigation'

export type RouteVisibility = 'sidebar' | 'hidden'

export type RouteTitleKey =
  | 'overview'
  | 'login'
  | 'import'
  | 'accounts'
  | 'articles'
  | 'albums'
  | 'savedQueries'
  | 'jobs'
  | 'exports'
  | 'settings'

export interface RouteEntry {
  readonly key: string
  readonly href: string
  readonly visibility: RouteVisibility
  readonly titleKey: RouteTitleKey
  readonly group?: NavigationGroup
}

const sidebarRoutes: readonly RouteEntry[] = navigationItems.map((item) => ({
  key: item.key,
  href: item.href,
  visibility: 'sidebar',
  titleKey: sidebarTitleKey(item),
  group: item.group
}))

const hiddenRoutes: readonly RouteEntry[] = [
  { key: 'login', href: '/login', visibility: 'hidden', titleKey: 'login' },
  { key: 'import', href: '/import', visibility: 'hidden', titleKey: 'import' },
  { key: 'saved-queries', href: '/saved-queries', visibility: 'hidden', titleKey: 'savedQueries' }
]

export const routes: readonly RouteEntry[] = [...sidebarRoutes, ...hiddenRoutes]

export function matchRoute(pathname: string): RouteEntry | undefined {
  return routes.find((route) => route.href === pathname)
}

export function sidebarRouteEntries(): readonly RouteEntry[] {
  return routes.filter((route) => route.visibility === 'sidebar')
}

export function isKnownRoute(pathname: string): boolean {
  return matchRoute(pathname) !== undefined
}

function sidebarTitleKey(item: NavigationItem): RouteTitleKey {
  return item.key as RouteTitleKey
}

export { navigationGroups }
export type { NavigationGroup }
