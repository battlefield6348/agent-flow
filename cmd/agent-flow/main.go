package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
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

	cfg, err := orchestrator.LoadStartupConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err, "path", *configPath)
		os.Exit(1)
	}

	logDir := cfg.Logs.Path
	settings, err := orchestrator.LoadWorkflowSettings(cfg.SettingsPath)
	if err != nil {
		slog.Error("Failed to load workflow settings", "error", err)
		os.Exit(1)
	}
	gitlabURL := settings.GitLabURL

	gitlabRepo := orchestrator.NewHttpGitLabRepository(gitlabURL, "")
	workspaceRepo := orchestrator.NewOsWorkspaceRepository()
	terminal := orchestrator.NewTmuxTerminal()
	workerManager := orchestrator.NewWorkerManager(settings.Agents, logDir, terminal)

	service := orchestrator.NewOrchestratorService(gitlabRepo, workspaceRepo, workerManager)
	slog.Info("Starting local Workers in tmux...")
	workerManager.StartAll()

	interval := time.Duration(settings.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	scheduler := orchestrator.NewScheduler(service, interval, settings.AllowedProjects, settings.AllowedMRAuthors, settings.Agents, gitlabURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx)
	slog.Info("Agent Flow web UI is active", "address", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, orchestrator.NewWebServer(cfg.SettingsPath, workerManager, scheduler)); err != nil {
		slog.Error("Web server stopped", "error", err)
	}
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
