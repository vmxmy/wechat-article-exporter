export const zhCN = {
  product: {
    name: '文章导出器',
    local: '本地工作区',
    privacy: '仅限本机回环访问 · 数据保留在本地'
  },
  navigation: {
    workspace: '工作区',
    library: '资料库',
    operations: '操作',
    overview: '概览',
    articles: '文章',
    albums: '专辑',
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
    nextDescription: '使用文章视图查询受限分页，无需将完整归档加载到浏览器。'
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
  }
} as const
