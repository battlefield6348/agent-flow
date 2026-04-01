package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gemini-collaborator-go/internal/orchestrator"
	"gemini-collaborator-go/internal/telegram"
)

func main() {
	// 0. 解析命令列參數
	configPath := flag.String("config", "configs/config.yaml", "Path to config file")
	flag.Parse()

	// 1. 載入設定檔
	cfg, err := orchestrator.LoadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config from %s, using defaults: %v", *configPath, err)
	}

	// 2. 啟動背景 Workers (Collaborators)
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

	// 3. 啟動 Telegram Bot (如果設定存在)
	if cfg != nil && cfg.Telegram.Token != "" {
		tgBot, err := telegram.NewBot(cfg.Telegram.Token, cfg.Telegram.AllowedChatIDs, cfg.Collaborators)
		if err != nil {
			log.Printf("Failed to initialize Telegram Bot: %v", err)
		} else {
			go tgBot.Start()
			
			// 啟動日誌轉發
			poller := telegram.NewLogPoller(tgBot, logDir, cfg.Telegram.AllowedChatIDs)
			poller.Start()
			fmt.Println("[Collaborator] Telegram Bot and Log Poller started.")
		}
	}

	// 優化：優雅地關閉
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

	// 4. 保持主程序運行
	fmt.Println("[Collaborator] Orchestrator is running. Press Ctrl+C to stop.")
	select {}
}
