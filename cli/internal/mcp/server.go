package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

type Server struct {
	adapter *Adapter
}

type ServerOptions struct {
	ApplicationOptions Options
}

func NewServer(adapter *Adapter) *Server { return &Server{adapter: adapter} }

func (server *Server) ServeStdio(ctx context.Context) error {
	return server.Serve(ctx, os.Stdin, os.Stdout, os.Stderr)
}

type requestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type responseEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (server *Server) Serve(ctx context.Context, input io.Reader, output, logs io.Writer) error {
	if server == nil || server.adapter == nil || server.adapter.application == nil {
		return errors.New("local MCP server requires a shared application")
	}
	if input == nil || output == nil {
		return errors.New("local MCP stdio requires input and output streams")
	}
	if logs == nil {
		logs = io.Discard
	}
	scanner := bufio.NewScanner(input)
	limit := server.adapter.maxMessage
	minimumResponseSize := len(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"request exceeds maximum message size"}}`)
	if limit < minimumResponseSize {
		limit = minimumResponseSize
	}
	initial := 64 * 1024
	if limit < initial {
		initial = limit
	}
	scanner.Buffer(make([]byte, initial), limit+1)
	writer := bufio.NewWriter(output)
	defer writer.Flush()
	stopCloser := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(stopCloser) }) }
	defer stop()
	if closer, ok := input.(io.Closer); ok {
		go func() {
			select {
			case <-ctx.Done():
				_ = closer.Close()
			case <-stopCloser:
			}
		}()
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if len(line) > limit {
			server.log(logs, "rejected oversized JSON-RPC message")
			if err := writeResponse(writer, responseEnvelope{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32600, Message: "request exceeds maximum message size"}}, limit); err != nil {
				return err
			}
			continue
		}
		response, reply := server.handle(ctx, line)
		if !reply {
			continue
		}
		server.logResponse(logs, response)
		if err := writeResponse(writer, response, limit); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		server.log(logs, "server stopped by context cancellation")
		return err
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) || strings.Contains(err.Error(), "token too long") {
			server.log(logs, "closed after oversized JSON-RPC message")
			_ = writeResponse(writer, responseEnvelope{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32600, Message: "request exceeds maximum message size"}}, limit)
			return nil
		}
		return err
	}
	return nil
}

func (server *Server) handle(ctx context.Context, message []byte) (responseEnvelope, bool) {
	response := responseEnvelope{JSONRPC: "2.0", ID: json.RawMessage("null")}
	var request requestEnvelope
	if err := json.Unmarshal(message, &request); err != nil {
		response.Error = &rpcError{Code: -32700, Message: "parse error"}
		return response, true
	}
	if request.JSONRPC != "2.0" || request.Method == "" {
		response.Error = &rpcError{Code: -32600, Message: "invalid request"}
		return response, true
	}
	isNotification := len(request.ID) == 0 || bytes.Equal(bytes.TrimSpace(request.ID), []byte("null"))
	if !isNotification {
		response.ID = append(json.RawMessage(nil), request.ID...)
	}
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    server.adapter.capabilities(),
			"serverInfo":      server.adapter.implementation(),
			"instructions":    "Local-only MCP over stdio. Long-running operations return persistent job IDs.",
		}
	case "notifications/initialized", "notifications/cancelled":
		return responseEnvelope{}, false
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		response.Result = map[string]any{"tools": server.adapter.Tools()}
	case "tools/call":
		var parameters struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &parameters); err != nil || normalizeToolName(parameters.Name) == "" {
			response.Error = &rpcError{Code: -32602, Message: "invalid tools/call parameters"}
			break
		}
		value, err := server.adapter.Call(ctx, normalizeToolName(parameters.Name), parameters.Arguments)
		response.Result = toolResult(value, err)
	default:
		response.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	if isNotification {
		return responseEnvelope{}, false
	}
	return response, true
}

func writeResponse(writer *bufio.Writer, response responseEnvelope, maximum int) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if maximum > 0 && len(encoded) > maximum {
		fallback := responseEnvelope{JSONRPC: "2.0", ID: response.ID,
			Error: &rpcError{Code: -32603, Message: "response exceeds maximum message size"}}
		encoded, err = json.Marshal(fallback)
		if err != nil {
			return err
		}
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return writer.Flush()
}

func (server *Server) log(output io.Writer, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	fmt.Fprintln(output, "mcp: "+message)
	server.adapter.logger.Debug(message)
}

func (server *Server) logResponse(output io.Writer, response responseEnvelope) {
	if response.Error != nil {
		server.log(output, response.Error.Message)
		return
	}
	result, ok := response.Result.(*sdk.CallToolResult)
	if ok && result.IsError && len(result.Content) > 0 {
		if text, textOK := result.Content[0].(*sdk.TextContent); textOK {
			server.log(output, safety.RedactText(text.Text))
		}
	}
}
