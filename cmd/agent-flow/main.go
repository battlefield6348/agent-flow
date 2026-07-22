package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"gemini-collaborator-go/internal/orchestrator"
)

func main() {
	// 初始化結構化日誌 (預設輸出到 Stdout)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	const (
		settingsPath = "data/settings.yaml"
		listenAddr   = "0.0.0.0:8081"
	)
	settings, err := orchestrator.LoadWorkflowSettings(settingsPath)
	if err != nil {
		slog.Error("Failed to load workflow settings", "error", err)
		os.Exit(1)
	}
	gitlabURL := settings.GitLabURL

	gitlabRepo := orchestrator.NewHttpGitLabRepository(gitlabURL, "")
	workspaceRepo := orchestrator.NewOsWorkspaceRepository()

	caoDispatcher := orchestrator.NewCaoDispatcher(settings.CaoBinPath, settings.CaoSessionName)
	service := orchestrator.NewOrchestratorService(gitlabRepo, workspaceRepo, caoDispatcher)
	service.SetCheckCISuccess(settings.CheckCISuccess)
	slog.Info("Initialized Agent Flow with CLI Agent Orchestrator (CAO)")

	interval := time.Duration(settings.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	scheduler := orchestrator.NewScheduler(service, interval, settings.AllowedProjects, settings.AllowedMRAuthors, settings.Agents, gitlabURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler.Start(ctx)
	slog.Info("Agent Flow web UI is active", "address", listenAddr)
	if err := http.ListenAndServe(listenAddr, orchestrator.NewWebServer(settingsPath, scheduler)); err != nil {
		slog.Error("Web server stopped", "error", err)
	}
}
