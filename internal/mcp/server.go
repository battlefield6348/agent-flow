package mcp

import (
"encoding/json"
"fmt"
"net/http"
"sync"
)

type Tool struct {
	Name        string
	Description string
	Handler     func(params json.RawMessage) (interface{}, error)
}

type Server struct {
	tools map[string]Tool
	mu    sync.RWMutex
}

func NewServer() *Server {
	return &Server{
		tools: make(map[string]Tool),
	}
}

func (s *Server) AddTool(name, desc string, handler func(json.RawMessage) (interface{}, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[name] = Tool{Name: name, Description: desc, Handler: handler}
}

func (s *Server) Start(addr string) error {
	http.HandleFunc("/mcp/tools", s.handleListTools)
	http.HandleFunc("/mcp/call", s.handleCallTool)
	
	fmt.Printf("[MCP] Server listening on %s...\n", addr)
	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	json.NewEncoder(w).Encode(s.tools)
}

func (s *Server) handleCallTool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fmt.Printf("[MCP] Agent calling tool: %s\n", req.Name)

	s.mu.RLock()
	tool, ok := s.tools[req.Name]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "tool not found", http.StatusNotFound)
		return
	}

	res, err := tool.Handler(req.Args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(res)
}
