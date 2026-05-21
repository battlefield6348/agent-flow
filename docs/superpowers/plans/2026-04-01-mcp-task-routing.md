# MCP 任務路由實作計畫 (Phase 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 實作 MCP Server 並整合至 Orchestrator，讓 Collaborators (Gemini CLI) 能透過工具領取任務、更新進度並獲取上下文。

**Architecture:** 
- 在 `internal/mcp` 實作符合 MCP 規範的 stdio Server。
- 建立 `collaborator-tools` 二進位檔，供 Gemini CLI 作為 MCP Server 載入。
- 透過 SQLite 實現 Orchestrator 與 MCP Server 之間的狀態同步。

**Tech Stack:** 
- Go 1.25+
- SQLite (modernc.org/sqlite)
- MCP Protocol (Stdio)

---

### Task 1: 實作 MCP 工具邏輯 (Internal MCP Package)

**Files:**
- Create: `internal/mcp/types.go`
- Create: `internal/mcp/tools.go`
- Modify: `internal/repository/task.go`

- [ ] **Step 1: 定義 MCP 請求與回應結構**
```go
package mcp

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}
```

- [ ] **Step 2: 實作 poll_available_tasks 邏輯**
在 `internal/mcp/tools.go` 實作根據標籤領取任務的邏輯。

- [ ] **Step 3: 實作 update_task_status 邏輯**
實作任務狀態轉移與結果回報。

- [ ] **Step 4: 實作 read_task_context 邏輯**
實作獲取任務詳細 Payload 的邏輯。

---

### Task 2: 實作 MCP Stdio Server Loop

**Files:**
- Create: `internal/mcp/server.go`

- [ ] **Step 1: 實作 Stdio 讀取與 JSON-RPC 解析迴圈**
```go
func (s *Server) Start() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req JSONRPCRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		// 處理請求並回傳
	}
}
```

- [ ] **Step 2: 實作 Tool List 宣告**
回應 `list_tools` 請求，宣告三個核心工具。

- [ ] **Step 3: 實作 Tool Call 派送**
根據工具名稱呼叫對應的處理器。

---

### Task 3: 建立 MCP Server 進入點 (Main)

**Files:**
- Create: `cmd/mcp-server/main.go`

- [ ] **Step 1: 實作二進位檔進入點**
讀取資料庫路徑並啟動 MCP Server。

- [ ] **Step 2: 修改 Makefile 支援編譯**
```makefile
build:
	go build -o collaborator ./cmd/collaborator
	go build -o mcp-server ./cmd/mcp-server
```

---

### Task 4: 整合與驗證

**Files:**
- Modify: `configs/config.yaml`
- Modify: `internal/orchestrator/worker.go`

- [ ] **Step 1: 更新 Worker 啟動參數**
確保 `gemini-cli` 啟動時能正確識別 `mcp-server` 作為 `collaborator-tools`。

- [ ] **Step 2: 執行整合測試**
啟動 Orchestrator，手動建立任務，觀察 Worker 是否能自動領取並執行。

---
