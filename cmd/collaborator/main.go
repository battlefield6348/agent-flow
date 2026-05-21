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
	// 0. 解析命令列參數
	configPath := flag.String("config", "configs/config.yaml", "Path to config file")
	flag.Parse()

	// 1. 載入設定檔
	cfg, err := orchestrator.LoadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config from %s, using defaults: %v", *configPath, err)
	}

	// 2. 確保資料庫目錄存在
	dbDir := "./db"
	if cfg != nil && cfg.Database.Path != "" {
		dbDir = filepath.Dir(cfg.Database.Path)
	}
	_ = os.MkdirAll(dbDir, 0755)

	// 3. 啟動背景 Workers (Collaborators)
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
			// 重要：將每個 Worker 的輸出綁定到 TG Bot
			for _, w := range manager.Workers {
				workerID := w.Config.ID
				w.SetOutputCallback(func(line string) {
					// 傳送給所有允許的會話 (或者您可以根據需要邏輯調整)
					for _, chatID := range cfg.Telegram.AllowedChatIDs {
						tgBot.SendMessage(chatID, fmt.Sprintf("*[%s]*\n%s", workerID, line))
					}
				})
			}

			go tgBot.Start()
			fmt.Println("[Collaborator] Telegram Bot started.")
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
