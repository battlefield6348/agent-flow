package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"gemini-collaborator-go/internal/orchestrator"
	"gemini-collaborator-go/internal/telegram"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := orchestrator.LoadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config from %s, using defaults: %v", *configPath, err)
	}

	dbDir := "./db"
	if cfg != nil && cfg.Database.Path != "" {
		dbDir = filepath.Dir(cfg.Database.Path)
	}
	_ = os.MkdirAll(dbDir, 0755)

	var manager *orchestrator.WorkerManager
	logDir := "./logs"
	if cfg != nil && cfg.Logs.Path != "" {
		logDir = cfg.Logs.Path
	}

	if cfg != nil && len(cfg.Collaborators) > 0 {
		manager = orchestrator.NewWorkerManager(cfg.Collaborators, logDir)
		manager.StartAll()
		fmt.Printf("[Collaborator] %d workers started in tmux sessions.\n", len(cfg.Collaborators))
	}

	if cfg != nil && cfg.Telegram.Token != "" {
		tgBot, err := telegram.NewBot(cfg.Telegram.Token, cfg.Telegram.AllowedChatIDs, cfg.Collaborators, manager)
		if err != nil {
			log.Printf("Failed to initialize Telegram Bot: %v", err)
		} else {
			go tgBot.Start()
			fmt.Println("[Collaborator] Telegram Bot started.")
		}
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		if manager != nil {
			manager.StopAll()
		}
		os.Exit(0)
	}()

	fmt.Println("[Collaborator] Orchestrator is running. Press Ctrl+C to stop.")
	select {}
}
