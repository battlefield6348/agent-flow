package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gemini-collaborator-go/internal/orchestrator"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	const settingsPath = "configs/config.yaml"
	settings, err := orchestrator.LoadWorkflowSettings(settingsPath)
	if err != nil {
		slog.Error("載入設定檔失敗", "error", err)
		os.Exit(1)
	}
	gitlabURL := settings.GitLabURL

	gitlabRepo := orchestrator.NewHttpGitLabRepository(gitlabURL, "")
	workspaceRepo := orchestrator.NewOsWorkspaceRepository()

	caoDispatcher := orchestrator.NewCaoDispatcher(settings.CaoBinPath, settings.CaoSessionName, settings.CaoServerURL)
	service := orchestrator.NewOrchestratorService(gitlabRepo, workspaceRepo, caoDispatcher)
	service.SetCheckCISuccess(settings.CheckCISuccess)
	slog.Info("成功初始化 Agent Flow (結合 CLI Agent Orchestrator)")

	interval := time.Duration(settings.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	scheduler := orchestrator.NewScheduler(service, interval, settings.AllowedProjects, settings.AllowedMRAuthors, settings.Agents, gitlabURL, settings.GitLabToken)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 依據設定檔動態檢查與啟動指定的 CAO Sessions
	if err := caoDispatcher.EnsureSessions(ctx, settings.Agents); err != nil {
		slog.Warn("檢查/啟動 CAO Sessions 時發生非阻斷式錯誤", "error", err)
	}

	scheduler.Start(ctx)
	slog.Info("Agent Flow 背景輪詢服務已啟動...")

	<-ctx.Done()
	slog.Info("收到關閉訊號，Agent Flow 服務正在停止...")
}
