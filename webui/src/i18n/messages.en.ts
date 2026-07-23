export const en = {
  product: {
    name: 'Article Exporter',
    local: 'Local workspace',
    privacy: 'Loopback-only · your data stays local'
  },
  navigation: {
    workspace: 'Workspace',
    library: 'Library',
    operations: 'Operations',
    overview: 'Overview',
    articles: 'Articles',
    albums: 'Albums',
    jobs: 'Jobs',
    settings: 'Settings'
  },
  a11y: {
    skip: 'Skip to workspace content'
  },
  localeSwitch: '切换至简体中文',
  connection: {
    connected: 'Connected to local workspace',
    unavailable: 'Local workspace unavailable',
    checking: 'Checking local workspace'
  },
  overview: {
    title: 'Your local article workspace',
    description: 'Manage library records, persistent jobs, and local exports from the profile shared with the CLI, TUI, and MCP adapter.',
    profileTitle: 'Profile and session',
    profileDescription: 'Session and profile details will appear here after the local API is available.',
    nextTitle: 'Start with your library',
    nextDescription: 'Use the Articles view to query bounded pages without loading your full archive into the browser.'
  },
  articles: {
    title: 'Articles',
    description: 'A server-paginated local library view. Sorting, selected rows, and visible columns remain in the browser; records stay in the local runtime.',
    search: 'Search articles',
    searchPlaceholder: 'Search title, account, or author',
    selected: 'selected',
    empty: 'No articles match this query.',
    loading: 'Loading local article page…',
    unavailable: 'The local article API is not available yet.',
    retry: 'Retry',
    previous: 'Previous page',
    next: 'Next page',
    page: (current: number, total: number) => `Page ${current} of ${total}`,
    pagination: 'Article pagination',
    visibleColumns: 'Visible article columns',
    selectAll: 'Select all visible articles',
    selectRow: (title: string) => `Select ${title}`,
    columns: {
      title: 'Title',
      account: 'Account',
      published: 'Published',
      status: 'Status'
    }
  }
} as const
