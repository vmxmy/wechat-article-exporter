export const zhCN = {
  product: {
    name: '文章导出器',
    local: '本地工作区',
    privacy: '仅限本机回环访问 · 数据保留在本地',
    beta: 'Beta',
    readOnly: '只读'
  },
  navigation: {
    workspace: '工作区',
    library: '资料库',
    operations: '操作',
    overview: '概览',
    accounts: '账号',
    articles: '文章',
    albums: '专辑',
    savedQueries: '已保存查询',
    jobs: '任务',
    settings: '设置'
  },
  a11y: {
    skip: '跳至工作区内容'
  },
  localeSwitch: 'Switch to English',
  connection: {
    connected: '已连接至本地工作区',
    unavailable: '本地工作区不可用',
    checking: '正在检查本地工作区'
  },
  overview: {
    title: '你的本地文章工作区',
    description: '在与 CLI、TUI 和 MCP 适配器共享的 profile 中管理资料库记录、持久任务和本地导出。',
    profileTitle: 'Profile 与会话',
    profileDescription: '本地 API 可用后，此处将显示会话和 profile 详情。',
    nextTitle: '从资料库开始',
    nextDescription: '使用文章视图查询受限分页，无需将完整归档加载到浏览器。',
    runtimeTitle: '运行时',
    sessionTitle: '会话',
    storageTitle: '本地存储',
    unavailable: '实时本地详情暂不可用。P0 API 正在接入期间，页面保持只读。',
    sessionAccount: '账号',
    sessionState: '状态',
    runtimeProfile: 'Profile',
    runtimeVersion: '版本',
    storageCounts: (accounts: number, articles: number, albums: number, jobs: number) => `${accounts} 个账号 · ${articles} 篇文章 · ${albums} 个专辑 · ${jobs} 个任务`
  },
  articles: {
    title: '文章',
    description: '本地资料库的服务端分页视图。排序、已选行和可见列保留在浏览器；记录继续留在本地运行时。',
    search: '搜索文章',
    searchPlaceholder: '搜索标题、账号或作者',
    selected: '已选择',
    empty: '没有匹配此查询的文章。',
    loading: '正在加载本地文章页…',
    unavailable: '本地文章 API 尚不可用。',
    retry: '重试',
    previous: '上一页',
    next: '下一页',
    page: (current: number, total: number) => `第 ${current} 页，共 ${total} 页`,
    pagination: '文章分页',
    visibleColumns: '可见文章列',
    selectAll: '选择当前页全部文章',
    selectRow: (title: string) => `选择 ${title}`,
    columns: {
      title: '标题',
      account: '账号',
      published: '发布时间',
      status: '状态'
    }
  },
  resources: {
    accounts: {
      title: '账号',
      description: '已保存的本地账号按受限服务端分页展示。此只读 Beta 暂不提供账号修改和同步控制。',
      loading: '正在加载本地账号页…', unavailable: '本地 accounts API 尚不可用。', empty: '没有与查询匹配的已保存账号。', retry: '重试', previous: '上一页', next: '下一页', page: (current: number, total: number) => `第 ${current} 页，共 ${total} 页`, pagination: '账号分页', selected: '已选择', selectAll: '选择当前页所有行', selectRow: (row: string) => `选择 ${row}`, visibleColumns: '可见账号列',
      columns: { name: '账号', alias: '别名', articles: '文章数', synced: '最近同步', state: '同步状态' }
    },
    albums: {
      title: '专辑',
      description: '此 Beta 仅供查看专辑元数据。遍历和下载操作会在对应 API 可用后提供。',
      loading: '正在加载本地专辑页…', unavailable: '本地 albums API 尚不可用。', empty: '没有与查询匹配的专辑。', retry: '重试', previous: '上一页', next: '下一页', page: (current: number, total: number) => `第 ${current} 页，共 ${total} 页`, pagination: '专辑分页', selected: '已选择', selectAll: '选择当前页所有行', selectRow: (row: string) => `选择 ${row}`, visibleColumns: '可见专辑列',
      columns: { name: '专辑', articles: '文章数', paid: '付费', description: '简介' }
    },
    jobs: {
      title: '任务',
      description: '持久任务快照每五秒刷新。此 P0 页面只观察共享本地任务，刻意不提供控制操作。',
      loading: '正在加载本地任务快照…', unavailable: '本地 jobs 快照 API 尚不可用。', empty: '尚未记录持久任务。', retry: '重试', previous: '上一页', next: '下一页', page: (current: number, total: number) => `第 ${current} 页，共 ${total} 页`, pagination: '任务分页', selected: '已选择', selectAll: '选择当前页所有行', selectRow: (row: string) => `选择 ${row}`, visibleColumns: '可见任务列',
      columns: { kind: '类型', state: '状态', created: '创建时间', updated: '更新时间', counts: '进度' }
    },
    savedQueries: {
      title: '已保存查询',
      description: '此处可查看已保存的查询定义。创建、修改和删除会在 mutation 契约发布前保持不可用。',
      loading: '正在加载已保存查询…', unavailable: '本地 saved-queries API 尚不可用。', empty: '尚未保存文章查询。', retry: '重试', previous: '上一页', next: '下一页', page: (current: number, total: number) => `第 ${current} 页，共 ${total} 页`, pagination: '已保存查询分页', selected: '已选择', selectAll: '选择当前页所有行', selectRow: (row: string) => `选择 ${row}`, visibleColumns: '可见已保存查询列',
      columns: { name: '名称', query: '查询条件', updated: '更新时间' }
    }
  }
} as const
