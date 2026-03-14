// Package provider implements the Agent Communication Protocol (ACP) types
// and provider interfaces used for JSON-RPC communication between Gas Town
// and AI agents (Claude Code, Gemini CLI, etc.).
package provider

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	// JSONRPCVersion is the JSON-RPC protocol version used by ACP.
	JSONRPCVersion = "2.0"
)

// Role represents a participant role in an ACP conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// ContentType identifies the kind of content in a message block.
type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeToolUse    ContentType = "tool_use"
	ContentTypeToolResult ContentType = "tool_result"
	ContentTypeImage      ContentType = "image"
)

// ToolType categorizes tools exposed via ACP.
type ToolType string

const (
	ToolTypeFunction ToolType = "function"
)

// JSONRPCRequest is a JSON-RPC 2.0 request message sent from client to server.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response message sent from server to client.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents an error object in a JSON-RPC response.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ContentBlock is a polymorphic content element within an ACP message.
// The Type field determines which other fields are populated (text, tool_use,
// tool_result, or image).
type ContentBlock struct {
	Type ContentType `json:"type"`

	Text string `json:"text,omitempty"`

	ToolUseID string `json:"tool_use_id,omitempty"`

	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Content any             `json:"content,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	Source  *ImageSource    `json:"source,omitempty"`
}

// ImageSource holds base64-encoded image data for image content blocks.
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// Message is a single turn in an ACP conversation, containing a role and content blocks.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// Tool describes a callable tool exposed to an agent via ACP.
type Tool struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	InputSchema *InputSchema `json:"input_schema,omitempty"`
}

// InputSchema defines the JSON Schema for a tool's input parameters.
// The Additional field captures any extra schema properties not explicitly modeled.
type InputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
	Additional map[string]any `json:"-"`
}

// MarshalJSON merges the standard schema fields with any Additional properties.
func (s *InputSchema) MarshalJSON() ([]byte, error) {
	type Alias InputSchema
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	data, err := json.Marshal(aux)
	if err != nil {
		return nil, err
	}
	if len(s.Additional) == 0 {
		return data, nil
	}
	var merged map[string]any
	if err := json.Unmarshal(data, &merged); err != nil {
		return nil, err
	}
	for k, v := range s.Additional {
		merged[k] = v
	}
	return json.Marshal(merged)
}

// UnmarshalJSON parses schema fields and captures unknown properties in Additional.
func (s *InputSchema) UnmarshalJSON(data []byte) error {
	type Alias InputSchema
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	delete(raw, "type")
	delete(raw, "properties")
	delete(raw, "required")
	if len(raw) > 0 {
		s.Additional = raw
	}
	return nil
}

// InitializeParams contains the parameters for the ACP initialize request.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocol_version"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"client_info"`
	Meta            map[string]any     `json:"_meta,omitempty"`
}

// ClientCapabilities advertises what features the ACP client supports.
type ClientCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
}

// ToolsCapability indicates whether tool list change notifications are supported.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability indicates whether resource list change notifications are supported.
type ResourcesCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ClientInfo identifies the ACP client (e.g., "gas-town", "1.0.0").
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is returned by the server after a successful initialize handshake.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocol_version"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"server_info"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ServerCapabilities advertises what features the ACP server supports.
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
}

// ServerInfo identifies the ACP server (agent name and version).
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ListToolsParams contains parameters for the tools/list request.
type ListToolsParams struct {
	Cursor string         `json:"cursor,omitempty"`
	Meta   map[string]any `json:"_meta,omitempty"`
}

// ListToolsResult is the response to a tools/list request.
type ListToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// CallToolParams contains parameters for the tools/call request.
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Meta      map[string]any `json:"_meta,omitempty"`
}

// CallToolResult is the response to a tools/call request.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// CreateMessageParams contains parameters for agent-initiated message creation (sampling).
type CreateMessageParams struct {
	Messages    []Message      `json:"messages"`
	Model       string         `json:"model,omitempty"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
	System      []ContentBlock `json:"system,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

// CreateMessageResult is the response to a sampling/createMessage request.
type CreateMessageResult struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
	Model   string         `json:"model,omitempty"`
	Usage   *Usage         `json:"usage,omitempty"`
}

// Usage tracks token consumption for a message exchange.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

// TextContent is a typed wrapper for text-only content blocks.
type TextContent struct {
	Type ContentType `json:"type"`
	Text string      `json:"text"`
}

// NewTextContent creates a ContentBlock with text content.
func NewTextContent(text string) ContentBlock {
	return ContentBlock{
		Type: ContentTypeText,
		Text: text,
	}
}

// ToolUseContent is a typed wrapper for tool invocation content blocks.
type ToolUseContent struct {
	Type  ContentType     `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// NewToolUseContent creates a ContentBlock representing a tool invocation.
func NewToolUseContent(id, name string, input any) (ContentBlock, error) {
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return ContentBlock{}, fmt.Errorf("marshal tool input: %w", err)
	}
	return ContentBlock{
		Type:  ContentTypeToolUse,
		Name:  name,
		Input: inputBytes,
	}, nil
}

// ToolResultContent is a typed wrapper for tool result content blocks.
type ToolResultContent struct {
	Type      ContentType `json:"type"`
	ToolUseID string      `json:"tool_use_id"`
	Content   any         `json:"content"`
	IsError   bool        `json:"is_error,omitempty"`
}

// NewToolResultContent creates a ContentBlock containing a tool's execution result.
func NewToolResultContent(toolUseID string, content any, isError bool) ContentBlock {
	return ContentBlock{
		Type:      ContentTypeToolResult,
		ToolUseID: toolUseID,
		Content:   content,
		IsError:   isError,
	}
}

// NewUserMessage creates a user-role Message with a single text block.
func NewUserMessage(text string) Message {
	return Message{
		Role:    RoleUser,
		Content: []ContentBlock{NewTextContent(text)},
	}
}

// NewUserMessageWithContent creates a user-role Message from arbitrary content blocks.
func NewUserMessageWithContent(blocks ...ContentBlock) Message {
	return Message{
		Role:    RoleUser,
		Content: blocks,
	}
}

// NewAssistantMessage creates an assistant-role Message with a single text block.
func NewAssistantMessage(text string) Message {
	return Message{
		Role:    RoleAssistant,
		Content: []ContentBlock{NewTextContent(text)},
	}
}

// NewAssistantMessageWithContent creates an assistant-role Message from arbitrary content blocks.
func NewAssistantMessageWithContent(blocks ...ContentBlock) Message {
	return Message{
		Role:    RoleAssistant,
		Content: blocks,
	}
}

// NewSystemMessage creates a system-role Message with a single text block.
func NewSystemMessage(text string) Message {
	return Message{
		Role:    RoleSystem,
		Content: []ContentBlock{NewTextContent(text)},
	}
}

// SimpleMessage is a higher-level envelope used for Gas Town internal messaging
// (mail, nudges). It maps to/from ACP Messages for agent delivery.
type SimpleMessage struct {
	ID        string    `json:"id,omitempty"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Priority  string    `json:"priority,omitempty"`
	Type      string    `json:"type,omitempty"`
}

// TranslateSimpleMessage converts a SimpleMessage into an ACP Message for agent consumption.
func TranslateSimpleMessage(sm SimpleMessage) Message {
	var content []ContentBlock
	if sm.Subject != "" && sm.Body != "" {
		content = []ContentBlock{
			NewTextContent(fmt.Sprintf("**%s**\n\n%s", sm.Subject, sm.Body)),
		}
	} else if sm.Body != "" {
		content = []ContentBlock{NewTextContent(sm.Body)}
	} else {
		content = []ContentBlock{NewTextContent("")}
	}
	return Message{
		Role:    RoleUser,
		Content: content,
	}
}

// TranslateMessageToSimple extracts text content from an ACP Message into a SimpleMessage.
func TranslateMessageToSimple(msg Message) SimpleMessage {
	var body string
	for _, block := range msg.Content {
		if block.Type == ContentTypeText && block.Text != "" {
			if body != "" {
				body += "\n"
			}
			body += block.Text
		}
	}
	return SimpleMessage{
		Body:      body,
		Timestamp: time.Now(),
	}
}

// ToolFromDefinition constructs a Tool from a name, description, and raw JSON schema map.
func ToolFromDefinition(name, description string, schema map[string]any) Tool {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		props = make(map[string]any)
	}

	return Tool{
		Name:        name,
		Description: description,
		InputSchema: &InputSchema{
			Type:       "object",
			Properties: props,
			Required:   getStringSlice(schema["required"]),
		},
	}
}

func getStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// NewInitializeRequest builds a JSON-RPC initialize request for the ACP handshake.
func NewInitializeRequest(id any, clientName, clientVersion string) JSONRPCRequest {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: ClientCapabilities{
			Tools: &ToolsCapability{ListChanged: true},
		},
		ClientInfo: ClientInfo{
			Name:    clientName,
			Version: clientVersion,
		},
	}
	paramsBytes, _ := json.Marshal(params)
	return JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  "initialize",
		Params:  paramsBytes,
	}
}

// NewInitializeResponse builds a JSON-RPC initialize response completing the handshake.
func NewInitializeResponse(id any, serverName, serverVersion, instructions string) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result: InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: ServerCapabilities{
				Tools: &ToolsCapability{ListChanged: false},
			},
			ServerInfo: ServerInfo{
				Name:    serverName,
				Version: serverVersion,
			},
			Instructions: instructions,
		},
	}
}

// NewListToolsRequest builds a JSON-RPC tools/list request.
func NewListToolsRequest(id any) JSONRPCRequest {
	return JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  "tools/list",
	}
}

// NewListToolsResponse builds a JSON-RPC tools/list response.
func NewListToolsResponse(id any, tools []Tool) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result: ListToolsResult{
			Tools: tools,
		},
	}
}

// NewCallToolRequest builds a JSON-RPC tools/call request.
func NewCallToolRequest(id any, name string, args map[string]any) JSONRPCRequest {
	paramsBytes, _ := json.Marshal(CallToolParams{
		Name:      name,
		Arguments: args,
	})
	return JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  "tools/call",
		Params:  paramsBytes,
	}
}

// NewCallToolResponse builds a JSON-RPC tools/call response.
func NewCallToolResponse(id any, content []ContentBlock, isError bool) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result: CallToolResult{
			Content: content,
			IsError: isError,
		},
	}
}

// NewErrorResponse builds a JSON-RPC error response with standard error codes.
func NewErrorResponse(id any, code int, message string, data any) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// Standard JSON-RPC 2.0 error codes.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// ParseRequest deserializes and validates a JSON-RPC request from raw bytes.
func ParseRequest(data []byte) (*JSONRPCRequest, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parse JSON-RPC request: %w", err)
	}
	if req.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("invalid JSON-RPC version: %s", req.JSONRPC)
	}
	return &req, nil
}

// ParseResponse deserializes and validates a JSON-RPC response from raw bytes.
func ParseResponse(data []byte) (*JSONRPCResponse, error) {
	var resp JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse JSON-RPC response: %w", err)
	}
	if resp.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("invalid JSON-RPC version: %s", resp.JSONRPC)
	}
	return &resp, nil
}

// ParseParams unmarshals the request's Params field into the provided value.
func (r *JSONRPCRequest) ParseParams(v any) error {
	if len(r.Params) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.Params, v); err != nil {
		return fmt.Errorf("parse params: %w", err)
	}
	return nil
}
