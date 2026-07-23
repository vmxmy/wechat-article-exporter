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
    login: '登录',
    import: '导入 URL',
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
  unavailableActions: {
    confirmationTitle: '需要确认',
    confirmationDescription: '破坏性或中断性操作会在执行前要求确认。',
    apiUnavailable: '此操作需要当前尚未提供的本地 API 端点。'
  },
  login: {
    title: '微信登录',
    description: '查看当前本地会话，或启动二维码登录流程。',
    sessionTitle: '当前会话',
    account: '账号',
    state: '状态',
    checking: '正在检查本地会话…',
    unavailable: '无法加载本地会话状态。',
    qrTitle: '二维码登录',
    qrDescription: '开始登录后，二维码及其轮询状态将显示在这里。',
    start: '开始二维码登录',
    poll: '轮询登录状态',
    complete: '完成登录',
    states: { authenticated: '已登录', unauthenticated: '未登录', waiting: '等待扫码', scanned: '已扫码', confirmed: '已确认', expired: '已过期', completed: '已完成' }
  },
  import: {
    title: '导入单篇文章 URL',
    description: '粘贴一个公开文章 URL，将其导入本地工作区。',
    url: '文章 URL',
    placeholder: 'https://mp.weixin.qq.com/s/…',
    submit: '导入 URL', force: '内容已存在时重新下载', queued: (id: string) => `已排队导入任务 ${id}。`, failed: '无法将 URL 加入队列。',
    note: '导入会创建持久化本地下载任务，不会将 URL 上传到项目运营的服务。'
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
    },
    actions: {
      title: '已选文章操作',
      description: '预览、下载和保存查询需要专门的本地端点。',
      preview: '预览所选文章',
      download: '下载所选文章',
      saveQuery: '保存当前查询'
    }
  },
  resources: {
    accounts: {
      title: '账号',
      description: '已保存的本地账号按受限服务端分页展示。此只读 Beta 暂不提供账号修改和同步控制。',
      loading: '正在加载本地账号页…', unavailable: '本地 accounts API 尚不可用。', empty: '没有与查询匹配的已保存账号。', retry: '重试', previous: '上一页', next: '下一页', page: (current: number, total: number) => `第 ${current} 页，共 ${total} 页`, pagination: '账号分页', selected: '已选择', selectAll: '选择当前页所有行', selectRow: (row: string) => `选择 ${row}`, visibleColumns: '可见账号列',
      columns: { name: '账号', alias: '别名', articles: '文章数', synced: '最近同步', state: '同步状态' },
      actions: { title: '账号操作', description: '发现账号、保存本地账号记录，或为一个已选账号启动同步。', search: '搜索发现', fakeid: '账号 fakeid', name: '账号名称', alias: '别名', discover: '发现账号', add: '保存账号', edit: '更新所选账号', remove: '删除所选账号', sync: '同步所选账号', selectOne: '请先仅选择一个账号。', deleteConfirm: '删除所选本地账号记录？此操作无法撤销。', actionFailed: '无法完成账号操作。' }
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
      columns: { kind: '类型', state: '状态', created: '创建时间', updated: '更新时间', counts: '进度' },
      actions: { title: '任务控制', description: '暂停、继续、重试或取消一个已选持久化任务。', start: '启动任务', pause: '暂停所选任务', resume: '继续所选任务', retry: '重试所选任务', cancel: '取消所选任务', selectOne: '请先仅选择一个任务。', confirmPause: '暂停此任务？', confirmRetry: '重试此任务？', confirmCancel: '取消此任务？这可能中断本地工作。', actionFailed: '无法完成任务操作。' }
    },
    savedQueries: {
      title: '已保存查询',
      description: '此处可查看已保存的查询定义。创建、修改和删除会在 mutation 契约发布前保持不可用。',
      loading: '正在加载已保存查询…', unavailable: '本地 saved-queries API 尚不可用。', empty: '尚未保存文章查询。', retry: '重试', previous: '上一页', next: '下一页', page: (current: number, total: number) => `第 ${current} 页，共 ${total} 页`, pagination: '已保存查询分页', selected: '已选择', selectAll: '选择当前页所有行', selectRow: (row: string) => `选择 ${row}`, visibleColumns: '可见已保存查询列',
      columns: { name: '名称', query: '查询条件', updated: '更新时间' },
      actions: { title: '已保存查询操作', description: '创建、编辑或删除已保存查询需要受支持的写入端点。', create: '保存查询', edit: '编辑所选查询', remove: '删除所选查询' }
    }
  }
} as const
