package processor

const (
	defaultMaxInputBytes          = 8 << 20
	defaultMaxScriptBytes         = 6 << 20
	defaultMaxPayloadBytes        = 6 << 20
	defaultMaxDecodedPayloadBytes = 12 << 20
	defaultMaxStringBytes         = 8 << 20
	defaultMaxDepth               = 96
	defaultMaxObjectMembers       = 200_000
	defaultMaxArrayItems          = 200_000
	defaultMaxHTMLDepth           = 256
	defaultMaxHTMLNodes           = 250_000
	defaultMaxResources           = 20_000
	defaultMaxOutputBytes         = 32 << 20
)

type Limits struct {
	MaxInputBytes          int64
	MaxScriptBytes         int
	MaxPayloadBytes        int
	MaxDecodedPayloadBytes int
	MaxStringBytes         int
	MaxDepth               int
	MaxObjectMembers       int
	MaxArrayItems          int
	MaxHTMLDepth           int
	MaxHTMLNodes           int
	MaxResources           int
	MaxOutputBytes         int
}

func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes:          defaultMaxInputBytes,
		MaxScriptBytes:         defaultMaxScriptBytes,
		MaxPayloadBytes:        defaultMaxPayloadBytes,
		MaxDecodedPayloadBytes: defaultMaxDecodedPayloadBytes,
		MaxStringBytes:         defaultMaxStringBytes,
		MaxDepth:               defaultMaxDepth,
		MaxObjectMembers:       defaultMaxObjectMembers,
		MaxArrayItems:          defaultMaxArrayItems,
		MaxHTMLDepth:           defaultMaxHTMLDepth,
		MaxHTMLNodes:           defaultMaxHTMLNodes,
		MaxResources:           defaultMaxResources,
		MaxOutputBytes:         defaultMaxOutputBytes,
	}
}

func (limits Limits) withDefaults() Limits {
	defaults := DefaultLimits()
	if limits.MaxInputBytes <= 0 {
		limits.MaxInputBytes = defaults.MaxInputBytes
	}
	if limits.MaxScriptBytes <= 0 {
		limits.MaxScriptBytes = defaults.MaxScriptBytes
	}
	if limits.MaxPayloadBytes <= 0 {
		limits.MaxPayloadBytes = defaults.MaxPayloadBytes
	}
	if limits.MaxDecodedPayloadBytes <= 0 {
		limits.MaxDecodedPayloadBytes = defaults.MaxDecodedPayloadBytes
	}
	if limits.MaxStringBytes <= 0 {
		limits.MaxStringBytes = defaults.MaxStringBytes
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = defaults.MaxDepth
	}
	if limits.MaxObjectMembers <= 0 {
		limits.MaxObjectMembers = defaults.MaxObjectMembers
	}
	if limits.MaxArrayItems <= 0 {
		limits.MaxArrayItems = defaults.MaxArrayItems
	}
	if limits.MaxHTMLDepth <= 0 {
		limits.MaxHTMLDepth = defaults.MaxHTMLDepth
	}
	if limits.MaxHTMLNodes <= 0 {
		limits.MaxHTMLNodes = defaults.MaxHTMLNodes
	}
	if limits.MaxResources <= 0 {
		limits.MaxResources = defaults.MaxResources
	}
	if limits.MaxOutputBytes <= 0 {
		limits.MaxOutputBytes = defaults.MaxOutputBytes
	}
	return limits
}
