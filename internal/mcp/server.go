package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// MCPRequest JSON-RPC 2.0 基礎結構
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type ToolHandler func(params json.RawMessage) (interface{}, error)

type Server struct {
	tools map[string]ToolHandler
	mu    sync.RWMutex
}

func NewServer() *Server {
	return &Server{
		tools: make(map[string]ToolHandler),
	}
}

func (s *Server) RegisterTool(name string, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[name] = handler
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	res := s.handleRequest(req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleRequest(req MCPRequest) MCPResponse {
	s.mu.RLock()
	handler, ok := s.tools[req.Method]
	s.mu.RUnlock()

	if !ok {
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   map[string]string{"code": "-32601", "message": "Method not found"},
		}
	}

	result, err := handler(req.Params)
	if err != nil {
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   map[string]string{"code": "-32000", "message": err.Error()},
		}
	}

	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) Start(addr string) error {
	fmt.Printf("MCP Server listening on %s...\n", addr)
	return http.ListenAndServe(addr, s)
}
