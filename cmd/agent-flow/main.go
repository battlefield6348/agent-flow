package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gemini-collaborator-go/internal/orchestrator"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := orchestrator.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logDir := cfg.Logs.Path
	if logDir == "" {
		logDir = "./logs"
	}

	// 1. 初始化基礎設施 (Infrastructure)
	token := getGitLabToken()
	gitlabURL := cfg.Scheduler.GitLabURL
	if gitlabURL == "" {
		gitlabURL = "https://git.efaipd.com"
	}

	gitlabRepo := orchestrator.NewHttpGitLabRepository(gitlabURL, token)
	workspaceRepo := orchestrator.NewOsWorkspaceRepository()
	workerManager := orchestrator.NewWorkerManager(cfg.Collaborators, logDir)

	// 2. 初始化業務服務 (Use Case)
	service := orchestrator.NewOrchestratorService(gitlabRepo, workspaceRepo, workerManager)

	// 3. 啟動 Worker
	fmt.Println("Starting local Workers in tmux...")
	workerManager.StartAll()

	// 4. 啟動排程器 (Scheduler)
	interval := time.Duration(cfg.Scheduler.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	
	scheduler := orchestrator.NewScheduler(service, interval, cfg.Scheduler.AllowedProjects, cfg.Scheduler.AllowedMRAuthors)
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 偵測 GitLab 使用者名稱並輸出
	if username, err := gitlabRepo.GetUsername(ctx); err == nil {
		fmt.Printf("[Scheduler] Detected username from token: %s\n", username)
	}

	go scheduler.Start(ctx)

	fmt.Println("Local Review Monitor Mode is ACTIVE.")
	fmt.Println("Waiting for GitLab review targets...")

	// 5. 輸出監控邏輯 (CLI Delivery)
	monitorAnswers(logDir)
}

func getGitLabToken() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	tokenPath := filepath.Join(homeDir, ".gemini/antigravity/gitlab_token")
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(tokenBytes))
}

func monitorAnswers(logDir string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		// 檢查是否有任何 worker 的回答檔案
		files, err := os.ReadDir(logDir)
		if err != nil {
			continue
		}

		for _, f := range files {
			if strings.HasSuffix(f.Name(), "_answer.txt") {
				path := filepath.Join(logDir, f.Name())
				data, err := os.ReadFile(path)
				if err == nil {
					content := strings.TrimSpace(string(data))
					if content != "" && !strings.Contains(content, "NO_TASKS") {
						workerID := strings.TrimSuffix(f.Name(), "_answer.txt")
						fmt.Printf("\n==================== %s ANSWER ====================\n%s\n=========================================================\n\n", strings.ToUpper(workerID), content)
						_ = os.WriteFile(path, []byte(""), 0644)
					}
				}
			}
		}
	}
}
