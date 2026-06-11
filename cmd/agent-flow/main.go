package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gemini-collaborator-go/internal/orchestrator"
	"gemini-collaborator-go/internal/telegram"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := orchestrator.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	ensureMCPRegistered(cfg.Database.Path)

	logDir := cfg.Logs.Path
	if logDir == "" {
		logDir = "./logs"
	}
	manager := orchestrator.NewWorkerManager(cfg.Collaborators, logDir)

	fmt.Println("Starting local Workers in tmux...")
	manager.StartAll()

	bot, err := telegram.NewBot(cfg.Telegram.Token, cfg.Telegram.AllowedChatIDs, cfg.Collaborators, manager)
	if err != nil {
		fmt.Printf("Failed to initialize Telegram Bot: %v\n", err)
		manager.StopAll()
		os.Exit(1)
	}

	fmt.Println("Telegram War Room Mode is ACTIVE.")
	fmt.Println("Connect to your Telegram group and start collaborating!")

	bot.Start()
}

func ensureMCPRegistered(dbPath string) {
	serverPath := "./mcp-server"
	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		fmt.Println("Warning: mcp-server binary not found. Please run 'make build' first.")
		return
	}

	absServer, _ := filepath.Abs(serverPath)
	absDB, _ := filepath.Abs(dbPath)

	// 註冊或更新 MCP Server 設定
	fmt.Printf("Ensuring MCP server 'collaborator-tools' is registered (DB: %s)...\n", absDB)
	cmd := exec.Command("gemini", "mcp", "add", "collaborator-tools", absServer, "--db", absDB)
	if err := cmd.Run(); err != nil {
		fmt.Printf("Note: MCP registration might have skipped or failed: %v\n", err)
	}
}
