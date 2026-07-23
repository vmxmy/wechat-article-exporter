package mcp

// Tool describes one local MCP tool using the protocol's stable JSON shape.
// The local stdio server owns these protocol types so it does not pull in any
// HTTP transport or OAuth implementation.
type Tool struct {
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	InputSchema  any              `json:"inputSchema"`
	OutputSchema any              `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations `json:"annotations,omitempty"`
}

type ToolAnnotations struct {
	ReadOnlyHint    bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type CallToolResult struct {
	Content           []TextContent `json:"content"`
	IsError           bool          `json:"isError,omitempty"`
	StructuredContent any           `json:"structuredContent,omitempty"`
}

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Title   string `json:"title,omitempty"`
}

type ServerCapabilities struct {
	Tools        *ToolCapabilities `json:"tools,omitempty"`
	Experimental map[string]any    `json:"experimental,omitempty"`
}

type ToolCapabilities struct {
	ListChanged bool `json:"listChanged"`
}

type InitializeResult struct {
	ProtocolVersion string              `json:"protocolVersion"`
	Capabilities    *ServerCapabilities `json:"capabilities"`
	ServerInfo      *Implementation     `json:"serverInfo"`
	Instructions    string              `json:"instructions,omitempty"`
}
