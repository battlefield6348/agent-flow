package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gemini-collaborator-go/internal/orchestrator"
)

func main() {
	// 初始化結構化日誌 (預設輸出到 Stdout)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	configPath := flag.String("config", "configs/config.yaml", "Path to config file")
	reviewURL := flag.String("review-url", "", "Manually dispatch reviewer to a specific merge request URL")
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
	gitlabURL := cfg.Scheduler.GitLabURL
	workspaceRepo := orchestrator.NewOsWorkspaceRepository()
	terminal := orchestrator.NewTmuxTerminal()
	workerManager := orchestrator.NewWorkerManager(cfg.Collaborators, logDir, terminal)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *reviewURL != "" {
		if err := runManualReviewer(ctx, cfg, workspaceRepo, terminal, workerManager, gitlabURL, *reviewURL); err != nil {
			slog.Error("Manual reviewer run failed", "error", err, "review_url", *reviewURL)
			os.Exit(1)
		}
		return
	}

	token := cfg.Collaborators[0].GitLabToken
	gitlabRepo := orchestrator.NewHttpGitLabRepository(gitlabURL, token)
	service := orchestrator.NewOrchestratorService(gitlabRepo, workspaceRepo, workerManager)
	if cfg.Scheduler.CheckCISuccess != nil {
		service.SetCheckCISuccess(*cfg.Scheduler.CheckCISuccess)
	}

	slog.Info("Starting local Workers in tmux...")
	workerManager.StartAll()

	interval := time.Duration(cfg.Scheduler.IntervalSeconds) * time.Second
	scheduler := orchestrator.NewScheduler(service, interval, cfg.Scheduler.AllowedProjects, cfg.Scheduler.AllowedMRAuthors, cfg.Collaborators, gitlabURL)

	if username, err := gitlabRepo.GetUsername(ctx); err == nil {
		slog.Info("Detected GitLab user", "username", username)
	}

	go scheduler.Start(ctx)

	slog.Info("Local Review Monitor Mode is ACTIVE. Waiting for GitLab review targets...")

	monitorAnswers(logDir)
}

func runManualReviewer(ctx context.Context, cfg *orchestrator.Config, workspaceRepo orchestrator.WorkspaceRepository, terminal orchestrator.Terminal, workerManager *orchestrator.WorkerManager, gitlabURL, reviewURL string) error {
	projectPath, mrIID, err := parseMergeRequestURL(gitlabURL, reviewURL)
	if err != nil {
		return err
	}

	reviewerCfg, err := findCollaborator(cfg.Collaborators, "reviewer")
	if err != nil {
		return err
	}

	gitlabRepo := orchestrator.NewHttpGitLabRepository(gitlabURL, reviewerCfg.GitLabToken)
	service := orchestrator.NewOrchestratorService(gitlabRepo, workspaceRepo, workerManager)
	if cfg.Scheduler.CheckCISuccess != nil {
		service.SetCheckCISuccess(*cfg.Scheduler.CheckCISuccess)
	}

	slog.Info("Starting reviewer worker in manual mode", "mr_url", reviewURL)
	if !workerManager.StartByID("reviewer") {
		return fmt.Errorf("reviewer worker not found in config")
	}
	defer workerManager.StopByID("reviewer")

	mr, err := gitlabRepo.FetchMergeRequest(ctx, projectPath, mrIID)
	if err != nil {
		return fmt.Errorf("fetch merge request failed: %w", err)
	}

	if err := service.AssignMergeRequestForAgent(ctx, "reviewer", gitlabRepo, projectPath, *mr); err != nil {
		return err
	}

	slog.Info("Manual reviewer run completed", "project", projectPath, "mr_iid", mrIID)
	return nil
}

func findCollaborator(collaborators []orchestrator.CollaboratorConfig, id string) (*orchestrator.CollaboratorConfig, error) {
	for _, col := range collaborators {
		if col.ID == id {
			c := col
			return &c, nil
		}
	}
	return nil, fmt.Errorf("collaborator %q not found in config", id)
}

func parseMergeRequestURL(gitlabURL, reviewURL string) (string, int, error) {
	base, err := url.Parse(gitlabURL)
	if err != nil {
		return "", 0, fmt.Errorf("invalid scheduler gitlab_url: %w", err)
	}
	target, err := url.Parse(reviewURL)
	if err != nil {
		return "", 0, fmt.Errorf("invalid review URL: %w", err)
	}
	if !strings.EqualFold(base.Host, target.Host) {
		return "", 0, fmt.Errorf("review URL host %q does not match configured GitLab host %q", target.Host, base.Host)
	}

	parts := strings.Split(strings.Trim(strings.TrimSpace(target.Path), "/"), "/")
	for i := 0; i < len(parts)-2; i++ {
		if parts[i] == "-" && parts[i+1] == "merge_requests" {
			projectPath := strings.Join(parts[:i], "/")
			if projectPath == "" {
				return "", 0, fmt.Errorf("review URL does not contain a project path")
			}
			mrIID, err := strconv.Atoi(parts[i+2])
			if err != nil {
				return "", 0, fmt.Errorf("invalid merge request IID in URL: %w", err)
			}
			return projectPath, mrIID, nil
		}
	}
	return "", 0, fmt.Errorf("review URL is not a GitLab merge request URL: %s", reviewURL)
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
