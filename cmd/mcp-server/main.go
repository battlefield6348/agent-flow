package main

import (
	"flag"
	"fmt"
	"gemini-collaborator-go/internal/mcp"
	"gemini-collaborator-go/internal/repository"
	"log"
	"os"
)

func main() {
	dbPath := flag.String("db", "db/collaborator.db", "Path to SQLite database")
	flag.Parse()

	// 確保資料庫目錄存在
	// 這裡由 Orchestrator 負責建立比較好，但為了強健性，我們也檢查一下
	repo, err := repository.NewSQLiteTaskRepository(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	handler := mcp.NewToolHandler(repo)
	server := mcp.NewServer(handler)

	log.Println("MCP Server starting...")
	server.Start()
}
