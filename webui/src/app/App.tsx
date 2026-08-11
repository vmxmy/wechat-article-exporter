import { MutationCache, QueryCache, QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query'
import { ApiError, isWeChatAuthError } from '../lib/api'
import { queryKeys } from '../lib/queries'
import ThemeProvider from '@/components/themes/theme-provider'
import { useEffect } from 'react'
import { Workspace } from './Workspace'
import { useLocale } from '../i18n'

const queryClient = new QueryClient({
  // A rejected session is evidence about the session, not a transient fault:
  // retrying it burns a request and delays the failure the UI needs to react to.
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => !(error instanceof ApiError && error.status === 401) && failureCount < 1,
      refetchOnWindowFocus: false,
      refetchOnReconnect: true,
      staleTime: 5_000
    },
    mutations: { retry: false }
  },
  // WeChat rejecting an operation is the freshest evidence available about that
  // session, so the cached status must be re-read instead of waiting out the
  // poll interval while the header still claims the account is signed in.
  queryCache: new QueryCache({ onError: (error) => refreshSessionOnWeChatAuthError(error) }),
  mutationCache: new MutationCache({ onError: (error) => refreshSessionOnWeChatAuthError(error) })
})

function refreshSessionOnWeChatAuthError(error: unknown) {
  if (!isWeChatAuthError(error)) return
  void queryClient.invalidateQueries({ queryKey: queryKeys.session, refetchType: 'active' })
}

export function App() {
  const [locale, setLocale] = useLocale()

  return (
    <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
      <QueryClientProvider client={queryClient}>
        <ReconnectInvalidation />
        <Workspace locale={locale} onLocaleChange={setLocale} />
      </QueryClientProvider>
    </ThemeProvider>
  )
}

function ReconnectInvalidation() {
  const client = useQueryClient()

  useEffect(() => {
    const refreshActiveQueries = () => {
      void client.invalidateQueries({ refetchType: 'active' })
    }
    window.addEventListener('online', refreshActiveQueries)
    return () => window.removeEventListener('online', refreshActiveQueries)
  }, [client])

  return null
}
