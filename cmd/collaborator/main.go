package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gemini-collaborator-go/internal/gitlab"
	"gemini-collaborator-go/internal/mcp"
	"gemini-collaborator-go/internal/orchestrator"
	"gemini-collaborator-go/internal/repository"
)

func main() {
	// 1. 載入設定檔
	cfgPath := "configs/config.yaml.example"
	cfg, err := orchestrator.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	fmt.Printf("Orchestrator starting for project ID: %s...\n", cfg.GitLab.ProjectID)

	// 2. 初始化 SQLite Repository
	repo, err := repository.NewSQLiteTaskRepository(cfg.Database.Path)
	if err != nil {
		log.Fatalf("failed to initialize repository: %v", err)
	}

	// 3. 初始化並啟動 MCP Server (內嵌工具中控)
	mcpServer := mcp.NewServer()
	orchestrator.RegisterMCPTools(mcpServer, repo)

	go func() {
		if err := mcpServer.Start(":8080"); err != nil {
			log.Fatalf("MCP Server failed: %v", err)
		}
	}()

	// 4. 初始化 GitLab Adapter
	gitlabAdapter, err := gitlab.NewAdapter(cfg.GitLab.BaseURL, cfg.GitLab.Token)
	if err != nil {
		log.Printf("[Warning] GitLab connection failed: %v. Continuing offline.", err)
	}

	// 5. 初始化並啟動 Workflow Engine
	if gitlabAdapter != nil && cfg.GitLab.ProjectID != "" {
		engine := orchestrator.NewWorkflowEngine(repo, gitlabAdapter, cfg.GitLab.ProjectID)

		// 預設採用 60 秒做為同步週期，這可以透過 cfg.GitLab.PollInterval 解析獲取
		go engine.Start(time.Minute)
	}

	// 6. 初始化 Worker 管理器
	manager := orchestrator.NewWorkerManager(cfg.Collaborators)

	// 7. 監聽系統訊號進行優雅停機
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 8. 啟動所有子程序
	go manager.StartAll()

	// 等待訊號
	sig := <-sigChan
	fmt.Printf("\nReceived signal %v. Cleaning up...\n", sig)

	manager.StopAll()
	fmt.Println("Orchestrator shutdown completed successfully.")
	os.Exit(0)
}
