package tui

import "strings"

func (model Model) zh() bool { return model.options.Language == "zh-CN" }

func (model Model) text(english, chinese string) string {
	if model.zh() {
		return chinese
	}
	return english
}

func (model Model) areaLabel(area Area) string {
	if model.zh() {
		switch area {
		case AreaHome:
			return "首页"
		case AreaAccounts:
			return "公众号"
		case AreaArticles:
			return "文章"
		case AreaAlbums:
			return "专辑"
		case AreaJobs:
			return "任务"
		case AreaExports:
			return "导出"
		case AreaSettings:
			return "设置"
		case AreaStorage:
			return "存储"
		case AreaDiagnostics:
			return "诊断"
		}
	}
	return englishAreaLabel(area)
}

func englishAreaLabel(area Area) string {
	switch area {
	case AreaHome:
		return "Home"
	case AreaAccounts:
		return "Accounts"
	case AreaArticles:
		return "Articles"
	case AreaAlbums:
		return "Albums"
	case AreaJobs:
		return "Jobs"
	case AreaExports:
		return "Exports"
	case AreaSettings:
		return "Settings"
	case AreaStorage:
		return "Storage"
	case AreaDiagnostics:
		return "Diagnostics"
	default:
		return string(area)
	}
}

func (model Model) actionLabel(kind string) string {
	if !model.zh() {
		return ""
	}
	labels := map[string]string{
		"login": "扫码登录", "logout": "退出登录", "account_sync": "同步公众号", "account_delete": "删除本地数据",
		"article_filter": "编辑组合筛选", "article_query_save": "保存当前查询", "article_query_load": "加载已保存查询",
		"article_query_list": "查看已保存查询", "article_query_delete": "删除已保存查询", "article_download": "下载所选文章",
		"article_export": "导出所选文章", "album_reverse": "逆序遍历全部", "album_download": "批量下载", "album_export": "导出专辑",
		"job_cancel": "取消", "garbage_plan": "生成垃圾回收计划", "garbage_apply": "执行垃圾回收",
		"display_language":             "切换语言",
		string(OperationAccountImport): "导入清单", string(OperationAccountExport): "导出清单", string(OperationAlbumTraverse): "顺序遍历全部",
		string(OperationJobLogs): "查看日志和租约", string(OperationJobPause): "暂停", string(OperationJobResume): "继续",
		string(OperationJobRetry): "重试", string(OperationRouteHealth): "路由健康状态", string(OperationExportConfig): "配置导出",
		string(OperationExportManifest): "结果清单", string(OperationExportVerify): "验证结果", string(OperationOpenExport): "打开导出目录",
		string(OperationCredentials): "查看凭据", string(OperationCredentialImport): "导入凭据", string(OperationCredentialCheck): "验证凭据",
		string(OperationCredentialRemove): "删除凭据", string(OperationProxies): "查看代理", string(OperationProxyAdd): "添加代理",
		string(OperationProxyEnable): "启用代理", string(OperationProxyDisable): "禁用代理", string(OperationProxyTest): "测试代理",
		string(OperationProxyRemove): "删除代理", string(OperationPreferences): "查看偏好设置", string(OperationPreferenceSet): "设置偏好",
		string(OperationBackup): "备份", string(OperationRestore): "恢复", string(OperationIntegrity): "完整性检查",
		string(OperationDiagnostics): "刷新诊断", string(OperationDiagnosticBundle): "创建诊断包", string(OperationArticleComments): "评论",
		string(OperationArticleMetrics): "数据指标", string(OperationArticleResources): "资源完整性",
	}
	return labels[kind]
}

func (model Model) localizeAction(action actionItem) actionItem {
	if label := model.actionLabel(action.Kind); label != "" {
		action.Label = label
	}
	if model.zh() {
		descriptions := map[string]string{
			"Remove the local WeChat session": "移除本地微信会话", "Authenticate directly with WeChat": "直接向微信完成认证",
			"Start incremental account synchronization": "启动增量同步", "Requires exact typed confirmation": "需要输入精确确认文本",
			"key=value pairs for all article query fields":      "使用 key=value 指定文章筛选条件",
			"Creates one persistent job for stable article IDs": "为稳定文章 ID 创建一个持久任务",
			"Choose English or Simplified Chinese":              "选择英文或简体中文",
		}
		if description := descriptions[action.Description]; description != "" {
			action.Description = description
		}
	}
	return action
}

func (model Model) localizeActions(actions []actionItem) []actionItem {
	for index := range actions {
		actions[index] = model.localizeAction(actions[index])
	}
	return actions
}

func (model Model) localizedInputLabel(purpose inputPurpose, english string) string {
	if !model.zh() {
		return english
	}
	labels := map[inputPurpose]string{
		inputSearch: "本地筛选", inputDiscoverAccount: "搜索微信公众号", inputSingleArticle: "单篇文章 URL",
		inputArticleQuery:  "文章筛选条件（JSON，或使用分号分隔的 key=value）",
		inputAccountImport: "公众号清单路径", inputAccountExport: "公众号清单导出路径", inputRestoreArchive: "备份文件路径",
		inputExportFormat: "导出格式", inputExportOutput: "导出目录（直接回车使用默认目录）",
		inputExportPolicy: "HTML 资源策略（best-effort 或 strict）", inputExportArchive: "HTML 批量归档文件（可选 .zip；留空则按文章建目录）",
		inputSavedQueryName: "保存的文章查询名称", inputLoadQueryName: "要加载的文章查询名称", inputDeleteQueryName: "要删除的文章查询名称",
		inputGCPreview: "按 Enter 生成新的垃圾回收计划", inputGCApply: "粘贴最新计划中的精确确认文本",
		inputCredentialFile: "凭据 JSON 文件路径", inputCredentialID: "凭据 ID", inputProxyName: "代理名称",
		inputProxyEndpoint: "代理端点 URL", inputProxyAuth: "代理授权（可选，隐藏）", inputProxyTarget: "代理名称或 ID",
		inputPreferenceKey: "偏好设置键", inputPreferenceValue: "偏好设置值", inputLanguage: "语言（en 或 zh-CN）", inputDiagnosticBundle: "诊断包导出路径",
	}
	if label, ok := labels[purpose]; ok {
		return label
	}
	return english
}

func (model Model) formatColumns(columns []string) []string {
	if !model.zh() {
		return columns
	}
	labels := map[string]string{"name": "名称", "alias": "别名", "fakeid": "FakeID", "articles": "文章数", "messages": "消息数",
		"last_sync": "上次同步", "completed": "已完成", "title": "标题", "author": "作者", "published": "发布时间",
		"account": "公众号", "type": "类型", "content": "正文", "comments": "评论", "original": "原创", "paid": "付费",
		"albums": "专辑", "metrics": "指标", "kind": "类型", "state": "状态", "progress": "进度", "throughput": "吞吐量",
		"updated": "更新时间", "id": "ID", "format": "格式", "provenance": "来源记录", "generation": "代次", "output": "导出目录"}
	localized := make([]string, len(columns))
	for index, column := range columns {
		localized[index] = labels[column]
		if strings.TrimSpace(localized[index]) == "" {
			localized[index] = column
		}
	}
	return localized
}

func (model Model) localizedConfirmationTitle() string {
	if !model.zh() {
		return model.confirm.Title
	}
	labels := map[string]string{
		"open_html":                     "打开本地 HTML 预览",
		"account_delete":                "删除本地公众号数据",
		"job_cancel":                    "取消持久任务",
		string(OperationRestore):        "恢复资料库备份",
		string(OperationGarbageCollect): "回收对象垃圾",
	}
	if label := labels[model.confirm.Action]; label != "" {
		return label
	}
	return model.confirm.Title
}

func (model Model) localizedConfirmationScope() string {
	if !model.zh() {
		return model.confirm.Scope
	}
	labels := map[string]string{
		"open_html":              "在本地浏览器中打开 1 篇已缓存文章",
		"account_delete":         "删除选中的本地公众号数据",
		"job_cancel":             "取消选中的持久任务",
		string(OperationRestore): "替换当前本地配置档案的资料库",
	}
	if label := labels[model.confirm.Action]; label != "" {
		return label
	}
	return model.confirm.Scope
}

func (model Model) localizedConfirmationRecoverability() string {
	if !model.zh() {
		return model.confirm.Recoverability
	}
	labels := map[string]string{
		"open_html":              "不会使用远程渲染器；确认后才会打开浏览器。",
		"account_delete":         "请先创建备份；共享对象会保留，未引用对象可由垃圾回收清理。",
		"job_cancel":             "已提交的工作会保留，安全检查点仍可使用。",
		string(OperationRestore): "强烈建议先创建恢复前备份。",
	}
	if label := labels[model.confirm.Action]; label != "" {
		return label
	}
	return model.confirm.Recoverability
}
