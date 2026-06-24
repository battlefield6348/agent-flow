package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gemini-collaborator-go/internal/orchestrator"
)

func main() {
	// 初始化結構化日誌 (預設輸出到 Stdout)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	configPath := flag.String("config", "configs/config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := orchestrator.LoadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err, "path", *configPath)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		slog.Error("Config validation failed", "error", err)
		os.Exit(1)
	}

	logDir := cfg.Logs.Path

	token := getGitLabToken()
	gitlabURL := cfg.Scheduler.GitLabURL

	gitlabRepo := orchestrator.NewHttpGitLabRepository(gitlabURL, token)
	workspaceRepo := orchestrator.NewOsWorkspaceRepository()
	terminal := orchestrator.NewTmuxTerminal()
	workerManager := orchestrator.NewWorkerManager(cfg.Collaborators, logDir, terminal)

	service := orchestrator.NewOrchestratorService(gitlabRepo, workspaceRepo, workerManager)
	if cfg.Scheduler.CheckCISuccess != nil {
		service.SetCheckCISuccess(*cfg.Scheduler.CheckCISuccess)
	}

	slog.Info("Starting local Workers in tmux...")
	workerManager.StartAll()

	interval := time.Duration(cfg.Scheduler.IntervalSeconds) * time.Second
	scheduler := orchestrator.NewScheduler(service, interval, cfg.Scheduler.AllowedProjects, cfg.Scheduler.AllowedMRAuthors, cfg.Collaborators, gitlabURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if username, err := gitlabRepo.GetUsername(ctx); err == nil {
		slog.Info("Detected GitLab user", "username", username)
	}

	go scheduler.Start(ctx)

	slog.Info("Local Review Monitor Mode is ACTIVE. Waiting for GitLab review targets...")

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
