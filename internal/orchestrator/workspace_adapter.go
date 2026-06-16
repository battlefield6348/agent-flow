package orchestrator

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// OsWorkspaceRepository 負責管理本地工作區路徑，並提供快取以優化查詢效能
type OsWorkspaceRepository struct {
	cache   map[string]string
	mu      sync.RWMutex
	scanned bool
}

func NewOsWorkspaceRepository() *OsWorkspaceRepository {
	return &OsWorkspaceRepository{
		cache: make(map[string]string),
	}
}

// FindLocalPath 尋找專案在本地的對應路徑，首次查詢時會掃描父目錄並快取結果
func (r *OsWorkspaceRepository) FindLocalPath(ctx context.Context, projectPath string) (string, error) {
	projectPathLower := strings.ToLower(strings.TrimSpace(projectPath))

	// 1. 嘗試從讀鎖快取中讀取以優化併發查詢
	r.mu.RLock()
	path, exists := r.cache[projectPathLower]
	r.mu.RUnlock()
	if exists {
		return path, nil
	}

	// 2. 若快取中沒有且尚未掃描，則使用寫鎖保護進行一次性掃描
	r.mu.Lock()
	defer r.mu.Unlock()

	// 雙重檢查 (Double-check Locking)
	if path, exists := r.cache[projectPathLower]; exists {
		return path, nil
	}

	if !r.scanned {
		if err := r.scanWorkspaces(ctx); err != nil {
			return "", err
		}
		r.scanned = true
	}

	// 3. 掃描完成後，再次從快取中讀取
	if path, exists := r.cache[projectPathLower]; exists {
		return path, nil
	}

	return "", fmt.Errorf("local workspace not found for project: %s", projectPath)
}

// scanWorkspaces 執行一次性的父目錄遍歷並解析 Git 遠端 URL，填充快取
func (r *OsWorkspaceRepository) scanWorkspaces(ctx context.Context) error {
	currentWd, err := os.Getwd()
	if err != nil {
		return err
	}
	projectsDir := filepath.Dir(currentWd)

	files, err := os.ReadDir(projectsDir)
	if err != nil {
		return err
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

		if detectedPath != "" {
			detectedPathLower := strings.ToLower(strings.TrimSpace(detectedPath))
			r.cache[detectedPathLower] = subDir
		}
	}

	return nil
}
