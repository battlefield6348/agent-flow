package orchestrator

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type OsWorkspaceRepository struct{}

func NewOsWorkspaceRepository() *OsWorkspaceRepository {
	return &OsWorkspaceRepository{}
}

func (r *OsWorkspaceRepository) FindLocalPath(ctx context.Context, projectPath string) (string, error) {
	currentWd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// 假設專案都在同一個父目錄下
	projectsDir := filepath.Dir(currentWd)

	files, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		subDir := filepath.Join(projectsDir, file.Name())
		gitDir := filepath.Join(subDir, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			continue
		}

		// 使用 Context 執行 Git 指令
		cmd := exec.CommandContext(ctx, "git", "-C", subDir, "remote", "get-url", "origin")
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		urlStr := strings.TrimSpace(string(out))
		urlStr = strings.TrimSuffix(urlStr, ".git")

		var detectedPath string
		if strings.HasPrefix(urlStr, "git@") {
			parts := strings.SplitN(urlStr, ":", 2)
			if len(parts) == 2 {
				detectedPath = parts[1]
			}
		} else if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
			parsed, err := url.Parse(urlStr)
			if err == nil {
				detectedPath = strings.TrimPrefix(parsed.Path, "/")
			}
		}

		if strings.ToLower(detectedPath) == strings.ToLower(projectPath) {
			return subDir, nil
		}
	}

	return "", fmt.Errorf("local workspace not found for project: %s", projectPath)
}
