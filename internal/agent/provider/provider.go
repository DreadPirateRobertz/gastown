package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ProviderState tracks the lifecycle state of an ACP provider connection.
type ProviderState string

const (
	StateDisconnected ProviderState = "disconnected"
	StateConnecting   ProviderState = "connecting"
	StateReady        ProviderState = "ready"
	StateBusy         ProviderState = "busy"
	StateError        ProviderState = "error"
)

// AgentStatus is a snapshot of an agent's current connection and identity state.
type AgentStatus struct {
	State     ProviderState `json:"state"`
	SessionID string        `json:"session_id,omitempty"`
	AgentName string        `json:"agent_name,omitempty"`
	Version   string        `json:"version,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// ToolCallback is invoked when the agent calls a tool registered with the provider.
type ToolCallback func(ctx context.Context, name string, args map[string]any) (CallToolResult, error)

// SessionStartCallback is invoked when an ACP session is initialized.
type SessionStartCallback func(ctx context.Context, info ServerInfo) error

// ACPProvider is the interface for ACP-compliant agent communication providers.
// Implementations handle the JSON-RPC handshake, tool dispatch, and message creation.
type ACPProvider interface {
	Initialize(ctx context.Context, clientName, clientVersion string) (*InitializeResult, error)
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResult, error)
	CreateMessage(ctx context.Context, params CreateMessageParams) (*CreateMessageResult, error)
	GetStatus() AgentStatus
	OnToolCall(callback ToolCallback)
	OnSessionStart(callback SessionStartCallback)
	Close() error
}

// ACPProviderConfig holds initialization parameters for creating an ACP provider.
type ACPProviderConfig struct {
	Name         string
	Version      string
	Instructions string
	Tools        []Tool
}

// BaseProvider implements shared ACP provider behavior (state management,
// tool registry, callbacks). Embed it to build concrete providers.
type BaseProvider struct {
	mu           sync.RWMutex
	state        ProviderState
	tools        []Tool
	toolCallback ToolCallback
	sessionStart SessionStartCallback
	status       AgentStatus
}

// NewBaseProvider creates a BaseProvider in disconnected state with the given config.
func NewBaseProvider(config ACPProviderConfig) *BaseProvider {
	return &BaseProvider{
		state: StateDisconnected,
		tools: config.Tools,
		status: AgentStatus{
			State:     StateDisconnected,
			AgentName: config.Name,
			Version:   config.Version,
		},
	}
}

func (p *BaseProvider) setState(state ProviderState) {
	p.mu.Lock()
	p.state = state
	p.status.State = state
	p.mu.Unlock()
}

// GetStatus returns a thread-safe snapshot of the provider's current status.
func (p *BaseProvider) GetStatus() AgentStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

// OnToolCall registers a callback for tool invocations from the agent.
func (p *BaseProvider) OnToolCall(callback ToolCallback) {
	p.mu.Lock()
	p.toolCallback = callback
	p.mu.Unlock()
}

// OnSessionStart registers a callback invoked when the ACP session initializes.
func (p *BaseProvider) OnSessionStart(callback SessionStartCallback) {
	p.mu.Lock()
	p.sessionStart = callback
	p.mu.Unlock()
}

// ListTools returns all tools registered with this provider.
func (p *BaseProvider) ListTools(ctx context.Context) ([]Tool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tools, nil
}

// AddTool registers a new tool with the provider.
func (p *BaseProvider) AddTool(tool Tool) {
	p.mu.Lock()
	p.tools = append(p.tools, tool)
	p.mu.Unlock()
}

// RemoveTool unregisters a tool by name.
func (p *BaseProvider) RemoveTool(name string) {
	p.mu.Lock()
	for i, t := range p.tools {
		if t.Name == name {
			p.tools = append(p.tools[:i], p.tools[i+1:]...)
			break
		}
	}
	p.mu.Unlock()
}

// LocalProvider is an in-process ACP provider that dispatches tool calls
// directly via callback without network transport.
type LocalProvider struct {
	*BaseProvider
	instructions string
}

// NewLocalProvider creates a LocalProvider with the given configuration.
func NewLocalProvider(config ACPProviderConfig) *LocalProvider {
	return &LocalProvider{
		BaseProvider: NewBaseProvider(config),
		instructions: config.Instructions,
	}
}

// Initialize completes the ACP handshake and transitions the provider to ready state.
func (p *LocalProvider) Initialize(ctx context.Context, clientName, clientVersion string) (*InitializeResult, error) {
	p.setState(StateReady)
	result := &InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{ListChanged: false},
		},
		ServerInfo: ServerInfo{
			Name:    p.status.AgentName,
			Version: p.status.Version,
		},
		Instructions: p.instructions,
	}
	p.mu.RLock()
	callback := p.sessionStart
	p.mu.RUnlock()
	if callback != nil {
		if err := callback(ctx, result.ServerInfo); err != nil {
			return nil, fmt.Errorf("session start callback: %w", err)
		}
	}
	return result, nil
}

// CallTool dispatches a tool invocation to the registered callback.
func (p *LocalProvider) CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResult, error) {
	p.mu.RLock()
	callback := p.toolCallback
	p.mu.RUnlock()
	if callback == nil {
		return &CallToolResult{
			Content: []ContentBlock{NewTextContent("no tool callback registered")},
			IsError: true,
		}, nil
	}
	result, err := callback(ctx, name, args)
	if err != nil {
		return &CallToolResult{
			Content: []ContentBlock{NewTextContent(err.Error())},
			IsError: true,
		}, nil
	}
	return &result, nil
}

// CreateMessage is not supported by LocalProvider; it returns an error.
func (p *LocalProvider) CreateMessage(ctx context.Context, params CreateMessageParams) (*CreateMessageResult, error) {
	return nil, fmt.Errorf("CreateMessage not supported for local provider")
}

// Close transitions the provider to disconnected state.
func (p *LocalProvider) Close() error {
	p.setState(StateDisconnected)
	return nil
}

// TranslateGastownMessage converts Gas Town mail fields into an ACP Message.
func TranslateGastownMessage(from, to, subject, body string) Message {
	var content string
	if subject != "" && body != "" {
		content = fmt.Sprintf("**Subject:** %s\n\n%s", subject, body)
	} else if subject != "" {
		content = subject
	} else if body != "" {
		content = body
	}
	return NewUserMessage(content)
}

// ExtractToolCalls returns all tool invocations from a message's content blocks.
func ExtractToolCalls(msg Message) []ToolCallInfo {
	var calls []ToolCallInfo
	for _, block := range msg.Content {
		if block.Type == ContentTypeToolUse {
			calls = append(calls, ToolCallInfo{
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}
	return calls
}

// ToolCallInfo captures a tool name and its input arguments from a message.
type ToolCallInfo struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ExtractToolResults returns all tool results from a message's content blocks.
func ExtractToolResults(msg Message) []ToolResultInfo {
	var results []ToolResultInfo
	for _, block := range msg.Content {
		if block.Type == ContentTypeToolResult {
			results = append(results, ToolResultInfo{
				ToolUseID: block.ToolUseID,
				Content:   block.Content,
				IsError:   block.IsError,
			})
		}
	}
	return results
}

// ToolResultInfo captures a tool's execution result from a message.
type ToolResultInfo struct {
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content"`
	IsError   bool   `json:"is_error"`
}

// ExtractTextContent concatenates all text blocks in a message into a single string.
func ExtractTextContent(msg Message) string {
	var text string
	for _, block := range msg.Content {
		if block.Type == ContentTypeText && block.Text != "" {
			if text != "" {
				text += "\n"
			}
			text += block.Text
		}
	}
	return text
}

// MessagesToJSON serializes a slice of Messages to JSON.
func MessagesToJSON(msgs []Message) ([]byte, error) {
	return json.Marshal(msgs)
}

// MessagesFromJSON deserializes a slice of Messages from JSON.
func MessagesFromJSON(data []byte) ([]Message, error) {
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("unmarshal messages: %w", err)
	}
	return msgs, nil
}

// RequestToJSON serializes a JSON-RPC request to bytes.
func RequestToJSON(req JSONRPCRequest) ([]byte, error) {
	return json.Marshal(req)
}

// ResponseToJSON serializes a JSON-RPC response to bytes.
func ResponseToJSON(resp JSONRPCResponse) ([]byte, error) {
	return json.Marshal(resp)
}

// ResponseFromJSON deserializes a JSON-RPC response from bytes.
func ResponseFromJSON(data []byte) (*JSONRPCResponse, error) {
	return ParseResponse(data)
}

// RequestFromJSON deserializes a JSON-RPC request from bytes.
func RequestFromJSON(data []byte) (*JSONRPCRequest, error) {
	return ParseRequest(data)
}

// NewInitializedNotification creates the notifications/initialized notification
// sent after a successful initialize handshake.
func NewInitializedNotification() JSONRPCRequest {
	return JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/initialized",
	}
}

// IsNotification returns true if the request is a JSON-RPC notification (no ID).
func IsNotification(req *JSONRPCRequest) bool {
	return req.ID == nil
}
