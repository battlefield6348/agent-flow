package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

type mockRepo struct{}

func (m *mockRepo) ListTasksByTags(tags []string, status interface{}) ([]interface{}, error) {
	return nil, nil
}

// ... simplified mock for testing

func TestServerHandleRequest(t *testing.T) {
	// 這裡簡單測試 list_tools 是否能正確回傳
	handler := &ToolHandler{}
	var input bytes.Buffer
	var output bytes.Buffer

	server := &Server{
		handler: handler,
		input:   &input,
		output:  &output,
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	b, _ := json.Marshal(req)
	input.Write(b)
	input.WriteString("\n")

	// 由於 Start() 是無限迴圈，我們手動呼叫 handleRequest
	server.handleRequest(&req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.ID.(float64) != 1 {
		t.Errorf("expected ID 1, got %v", resp.ID)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map")
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("tools is not an array")
	}

	// 預期有 3 個工具 (poll, update, read)
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}
}
