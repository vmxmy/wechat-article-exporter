import { useCallback, useEffect, useState } from 'react'
import { navigationEvent, replaceLocation } from '../app/navigation'
import { parsePagedBrowserView, serializePagedBrowserView } from './browserViewState'

export function usePagedBrowserView(): readonly [number, (pageIndex: number) => void] {
  const [pageIndex, setPageIndexState] = useState(() => parsePagedBrowserView(window.location.search).state.page - 1)
  const [pagePath] = useState(window.location.pathname)

  useEffect(() => {
    const initial = parsePagedBrowserView(window.location.search)
    if (initial.needsReplace) commitSearch(initial.canonicalSearch)
  }, [])

  useEffect(() => {
    const restore = () => {
      if (window.location.pathname !== pagePath) return
      const parsed = parsePagedBrowserView(window.location.search)
      if (parsed.needsReplace) commitSearch(parsed.canonicalSearch)
      setPageIndexState(parsed.state.page - 1)
    }
    window.addEventListener('popstate', restore)
    window.addEventListener(navigationEvent, restore)
    return () => {
      window.removeEventListener('popstate', restore)
      window.removeEventListener(navigationEvent, restore)
    }
  }, [pagePath])

  const setPageIndex = useCallback((nextPageIndex: number) => {
    setPageIndexState(nextPageIndex)
    commitSearch(serializePagedBrowserView({ page: nextPageIndex + 1 }, window.location.search))
  }, [])

  return [pageIndex, setPageIndex]
}

function commitSearch(search: string) {
  replaceLocation(`${window.location.pathname}${search}${window.location.hash}`)
}
