import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { LinkProvider } from '@astryxdesign/core/Link'
import { Theme as ThemeProvider } from '@astryxdesign/core/theme'
import { Workspace } from './Workspace'
import { RouterLink } from './RouterLink'
import { useLocale } from '../i18n'
import { workspaceTheme } from '../theme/workspaceTheme'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 5_000
    }
  }
})

export function App() {
  const [locale, setLocale] = useLocale()

  return (
    <ThemeProvider theme={workspaceTheme} mode="system">
      <LinkProvider component={RouterLink}>
        <QueryClientProvider client={queryClient}>
          <Workspace locale={locale} onLocaleChange={setLocale} />
        </QueryClientProvider>
      </LinkProvider>
    </ThemeProvider>
  )
}
