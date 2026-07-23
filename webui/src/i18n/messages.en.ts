export const en = {
  product: {
    name: 'Article Exporter',
    local: 'Local workspace',
    privacy: 'Loopback-only · your data stays local',
    beta: 'Beta',
    readOnly: 'Read-only'
  },
  navigation: {
    workspace: 'Workspace',
    library: 'Library',
    operations: 'Operations',
    overview: 'Overview',
    accounts: 'Accounts',
    articles: 'Articles',
    albums: 'Albums',
    savedQueries: 'Saved queries',
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
    nextDescription: 'Use the Articles view to query bounded pages without loading your full archive into the browser.',
    runtimeTitle: 'Runtime',
    sessionTitle: 'Session',
    storageTitle: 'Local storage',
    unavailable: 'Live local details are unavailable. The page remains read-only while the P0 API is rolling out.',
    sessionAccount: 'Account',
    sessionState: 'State',
    runtimeProfile: 'Profile',
    runtimeVersion: 'Version',
    storageCounts: (accounts: number, articles: number, albums: number, jobs: number) => `${accounts} accounts · ${articles} articles · ${albums} albums · ${jobs} jobs`
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
  },
  resources: {
    accounts: {
      title: 'Accounts',
      description: 'Saved local accounts are shown in bounded server pages. Account changes and sync controls are not available in this read-only beta.',
      loading: 'Loading local account page…', unavailable: 'The local accounts API is not available yet.', empty: 'No saved accounts match this query.', retry: 'Retry', previous: 'Previous page', next: 'Next page', page: (current: number, total: number) => `Page ${current} of ${total}`, pagination: 'Account pagination', selected: 'selected', selectAll: 'Select all rows on this page', selectRow: (row: string) => `Select ${row}`, visibleColumns: 'Visible account columns',
      columns: { name: 'Account', alias: 'Alias', articles: 'Articles', synced: 'Last synced', state: 'Sync state' }
    },
    albums: {
      title: 'Albums',
      description: 'Album metadata is available for inspection only in this beta. Traversal and download actions will arrive with their supported APIs.',
      loading: 'Loading local album page…', unavailable: 'The local albums API is not available yet.', empty: 'No albums match this query.', retry: 'Retry', previous: 'Previous page', next: 'Next page', page: (current: number, total: number) => `Page ${current} of ${total}`, pagination: 'Album pagination', selected: 'selected', selectAll: 'Select all rows on this page', selectRow: (row: string) => `Select ${row}`, visibleColumns: 'Visible album columns',
      columns: { name: 'Album', articles: 'Articles', paid: 'Paid', description: 'Description' }
    },
    jobs: {
      title: 'Jobs',
      description: 'Persistent job snapshots refresh every five seconds. This P0 view observes shared local jobs and intentionally exposes no controls.',
      loading: 'Loading local job snapshot…', unavailable: 'The local jobs snapshot API is not available yet.', empty: 'No persistent jobs are recorded.', retry: 'Retry', previous: 'Previous page', next: 'Next page', page: (current: number, total: number) => `Page ${current} of ${total}`, pagination: 'Job pagination', selected: 'selected', selectAll: 'Select all rows on this page', selectRow: (row: string) => `Select ${row}`, visibleColumns: 'Visible job columns',
      columns: { kind: 'Kind', state: 'State', created: 'Created', updated: 'Updated', counts: 'Progress' }
    },
    savedQueries: {
      title: 'Saved queries',
      description: 'Saved query definitions can be inspected here. Creating, changing, or deleting them is deliberately unavailable until mutation contracts ship.',
      loading: 'Loading saved queries…', unavailable: 'The local saved-queries API is not available yet.', empty: 'No saved article queries are recorded.', retry: 'Retry', previous: 'Previous page', next: 'Next page', page: (current: number, total: number) => `Page ${current} of ${total}`, pagination: 'Saved-query pagination', selected: 'selected', selectAll: 'Select all rows on this page', selectRow: (row: string) => `Select ${row}`, visibleColumns: 'Visible saved-query columns',
      columns: { name: 'Name', query: 'Query', updated: 'Updated' }
    }
  }
} as const
