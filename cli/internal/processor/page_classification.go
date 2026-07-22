package processor

import (
	"bytes"
	"strings"
)

func classifyKnownPage(html []byte) (Classification, bool) {
	// A valid article can quote one of these messages in its body. Only classify
	// terminal pages when the article root itself is absent.
	if containsArticleRoot(html) {
		return Classification{}, false
	}

	text := compactPageText(html)
	switch {
	case containsAny(text,
		"该内容已被发布者删除",
		"该内容已被作者删除",
		"the content has been deleted by the author",
	):
		return Classification{State: ClassificationDeleted, Reason: ReasonAuthorDeleted, Message: "article was deleted by its author"}, true
	case containsAny(text,
		"此内容因违规无法查看",
		"内容因违规无法查看",
		"涉嫌过度营销、骚扰用户",
	):
		return Classification{State: ClassificationUnavailable, Reason: ReasonPolicyViolation, Message: "article is unavailable because of a policy restriction"}, true
	case containsAny(text,
		"该内容暂时无法查看",
		"内容暂时无法查看",
		"this content is temporarily unavailable",
	):
		return Classification{State: ClassificationUnavailable, Reason: ReasonTemporarilyUnavailable, Message: "article is temporarily unavailable"}, true
	case containsAny(text,
		"请完成安全验证",
		"请进行安全验证",
		"需要完成验证",
		"security verification",
	):
		return Classification{State: ClassificationRiskControl, Reason: ReasonSecurityVerification, Message: "security verification is required"}, true
	case containsAny(text,
		"访问过于频繁",
		"操作过于频繁",
		"操作频繁",
		"请求过于频繁",
		"too many requests",
	):
		return Classification{State: ClassificationRiskControl, Reason: ReasonRateLimited, Message: "request was blocked by rate control"}, true
	case containsAny(text,
		"环境异常",
		"访问环境异常",
		"当前环境异常",
		"abnormal environment",
	):
		return Classification{State: ClassificationRiskControl, Reason: ReasonAbnormalEnvironment, Message: "request was blocked because of an abnormal environment"}, true
	case (bytes.Contains(bytes.ToLower(html), []byte("weui-msg")) || bytes.Contains(bytes.ToLower(html), []byte("mesg-block"))) && text != "":
		return Classification{State: ClassificationUnavailable, Reason: ReasonKnownUnavailable, Message: firstNRunes(text, 240)}, true
	default:
		return Classification{}, false
	}
}

func compactPageText(html []byte) string {
	var builder strings.Builder
	builder.Grow(min(len(html), 4096))
	inTag := false
	space := true
	for _, char := range string(html) {
		switch {
		case char == '<':
			inTag = true
		case char == '>':
			inTag = false
			if !space {
				builder.WriteByte(' ')
				space = true
			}
		case inTag:
			continue
		case char == '\n' || char == '\r' || char == '\t' || char == ' ':
			if !space {
				builder.WriteByte(' ')
				space = true
			}
		default:
			builder.WriteRune(char)
			space = false
		}
	}
	return strings.ToLower(strings.TrimSpace(builder.String()))
}

func containsAny(value string, options ...string) bool {
	for _, option := range options {
		if strings.Contains(value, strings.ToLower(option)) {
			return true
		}
	}
	return false
}

func firstNRunes(value string, count int) string {
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[:count])
}
