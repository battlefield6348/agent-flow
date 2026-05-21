package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type Server struct {
	handler *ToolHandler
	input   io.Reader
	output  io.Writer
}

func NewServer(handler *ToolHandler) *Server {
	return &Server{
		handler: handler,
		input:   os.Stdin,
		output:  os.Stdout,
	}
}

func (s *Server) Start() {
	scanner := bufio.NewScanner(s.input)
	for scanner.Scan() {
		line := scanner.Bytes()
		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error", nil)
			continue
		}

		s.handleRequest(&req)
	}
}

func (s *Server) handleRequest(req *JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		s.sendResponse(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"serverInfo": map[string]string{
				"name":    "collaborator-tools",
				"version": "0.1.0",
			},
		})
	case "tools/list":
		s.sendResponse(req.ID, ListToolsResult{
			Tools: s.handler.ListTools(),
		})
	case "tools/call":
		var callReq CallToolRequest
		if err := json.Unmarshal(req.Params, &callReq); err != nil {
			s.sendError(req.ID, -32602, "Invalid params", nil)
			return
		}

		result, err := s.handler.HandleCallTool(callReq)
		if err != nil {
			s.sendResponse(req.ID, CallToolResult{
				Content: []TextContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Error: %v", err),
					},
				},
			})
			return
		}

		s.sendResponse(req.ID, ToCallToolResult(result))
	case "notifications/initialized":
		// Ignore
	default:
		s.sendError(req.ID, -32601, "Method not found", nil)
	}
}

func (s *Server) sendResponse(id interface{}, result interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	b, _ := json.Marshal(resp)
	fmt.Fprintf(s.output, "%s\n", string(b))
}

func (s *Server) sendError(id interface{}, code int, message string, data interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: map[string]interface{}{
			"code":    code,
			"message": message,
			"data":    data,
		},
	}
	b, _ := json.Marshal(resp)
	fmt.Fprintf(s.output, "%s\n", string(b))
}
