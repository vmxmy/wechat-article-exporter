import { QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query'
import ThemeProvider from '@/components/themes/theme-provider'
import { useEffect } from 'react'
import { Workspace } from './Workspace'
import { useLocale } from '../i18n'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      refetchOnReconnect: true,
      staleTime: 5_000
    }
  }
})

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
