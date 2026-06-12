package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gemini-collaborator-go/internal/orchestrator"
)

type GitLabMR struct {
	IID         int    `json:"iid"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SHA         string `json:"sha"`
	WebURL      string `json:"web_url"`
	State       string `json:"state"`
	Author      struct {
		Username string `json:"username"`
	} `json:"author"`
}

type GitLabTodo struct {
	ID         int      `json:"id"`
	ActionName string   `json:"action_name"`
	TargetType string   `json:"target_type"`
	Target     GitLabMR `json:"target"`
	Project    struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
}

// 將 GitLab 上的特定待辦事項標記為已處理以避免重複掃描
func markTodoAsDone(gitlabURL, token string, todoID int) {
	apiURL := fmt.Sprintf("%s/api/v4/todos/%d/mark_as_done", gitlabURL, todoID)
	req, err := http.NewRequest("POST", apiURL, nil)
	if err != nil {
		fmt.Printf("[Scheduler] Error creating mark_as_done request: %v\n", err)
		return
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[Scheduler] Error executing mark_as_done: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		fmt.Printf("[Scheduler] Warning: mark_as_done returned status: %s\n", resp.Status)
	} else {
		fmt.Printf("[Scheduler] Todo %d marked as DONE.\n", todoID)
	}
}

func getProjectPathFromWebURL(webURL string) (string, error) {
	parsed, err := url.Parse(webURL)
	if err != nil {
		return "", err
	}
	path := parsed.Path
	path = strings.TrimPrefix(path, "/")
	idx := strings.Index(path, "/-/merge_requests")
	if idx == -1 {
		idx = strings.Index(path, "/merge_requests")
	}
	if idx == -1 {
		return "", fmt.Errorf("could not find merge requests segment in URL: %s", webURL)
	}
	return path[:idx], nil
}

func getGitLabUsername(gitlabURL, token string) (string, error) {
	apiURL := fmt.Sprintf("%s/api/v4/user", gitlabURL)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitLab User API status: %s", resp.Status)
	}

	var user struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}
	return user.Username, nil
}

func findLocalWorkspace(projectPath string) (string, error) {
	currentWd, err := os.Getwd()
	if err != nil {
		return "", err
	}
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

		cmd := exec.Command("git", "-C", subDir, "remote", "get-url", "origin")
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

func scanGitLabTodos(gitlabURL, token string, manager *orchestrator.WorkerManager, logDir string, allowedProjects, allowedMRAuthors []string) {
	apiURL := fmt.Sprintf("%s/api/v4/todos?state=pending&type=MergeRequest&per_page=100", gitlabURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		fmt.Printf("[Scheduler] Error creating HTTP request: %v\n", err)
		return
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[Scheduler] Error executing HTTP request: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[Scheduler] GitLab API returned non-OK status: %s\n", resp.Status)
		return
	}

	var todos []GitLabTodo
	if err := json.NewDecoder(resp.Body).Decode(&todos); err != nil {
		fmt.Printf("[Scheduler] Error parsing Todo JSON: %v\n", err)
		return
	}

	if len(todos) > 0 {
		fmt.Printf("[Scheduler] Fetching pending Todos from GitLab... (Total pending: %d)\n", len(todos))
	} else {
		fmt.Printf("[Scheduler] Scan complete: 0 pending Todos found.\n")
	}

	for _, todo := range todos {
		projectPath := todo.Project.PathWithNamespace
		mr := todo.Target

		// 若 Merge Request 已經被合併或關閉，則無需再評審，直接自動標記該 Todo 為已讀以清理 GitLab 待辦清單
		if strings.ToLower(mr.State) != "opened" {
			fmt.Printf("[Scheduler] Todo %d is associated with a non-opened MR %d [%s] (State: %s), auto-cleaning Todo...\n", todo.ID, mr.IID, projectPath, mr.State)
			markTodoAsDone(gitlabURL, token, todo.ID)
			continue
		}

		// 檢查是否在允許的專案白名單中，避免處理非維護專案的待辦
		if len(allowedProjects) > 0 {
			allowed := false
			for _, p := range allowedProjects {
				if strings.ToLower(strings.TrimSpace(p)) == strings.ToLower(projectPath) {
					allowed = true
					break
				}
			}
			if !allowed {
				// 輸出除錯資訊表示該專案不在白名單中而被過濾
				fmt.Printf("[Scheduler] Debug Todo %d [%s]: Skip (not in allowed_projects whitelist)\n", todo.ID, projectPath)
				continue
			}
		}

		// 檢查是否為指定的 MR 建立者白名單，避免掃描其他人建立的 MR
		if len(allowedMRAuthors) > 0 {
			authorAllowed := false
			mrAuthor := strings.ToLower(strings.TrimSpace(mr.Author.Username))
			for _, author := range allowedMRAuthors {
				if strings.ToLower(strings.TrimSpace(author)) == mrAuthor {
					authorAllowed = true
					break
				}
			}
			if !authorAllowed {
				// 輸出除錯資訊表示該 MR 建立者不在白名單中而被過濾
				fmt.Printf("[Scheduler] Debug Todo %d [%s] MR %d: Skip (MR author '%s' not in allowed_mr_authors whitelist)\n", todo.ID, projectPath, mr.IID, mr.Author.Username)
				continue
			}
		}

		// 檢查 reviewer 是否正在執行任務中以避免強行中斷
		reviewerBusy := false
		for _, w := range manager.Workers {
			if w.Config.ID == "reviewer" && w.IsBusy() {
				reviewerBusy = true
				break
			}
		}

		if reviewerBusy {
			fmt.Printf("[Scheduler] Reviewer is currently BUSY. Postponing Todo %d (MR %d) [%s] review until next scan...\n", todo.ID, mr.IID, projectPath)
			continue
		}

		fmt.Printf("[Scheduler] -> Target triggered: New pending Todo found for MR %d [%s] (Action: %s)\n", mr.IID, projectPath, todo.ActionName)
		fmt.Printf("[Scheduler] Resolving local workspace for MR %d [%s]...\n", mr.IID, projectPath)

		subDir, err := findLocalWorkspace(projectPath)
		if err != nil {
			fmt.Printf("[Scheduler] Error locating local workspace: %v\n", err)
			continue
		}

		for _, w := range manager.Workers {
			if w.Config.ID == "reviewer" {
				if w.Config.Workspace != subDir {
					fmt.Printf("[Scheduler] Switching reviewer workspace from '%s' to '%s'...\n", w.Config.Workspace, subDir)
					w.Stop()
					w.Config.Workspace = subDir
					w.Start()
					time.Sleep(15 * time.Second)
				}

				instruction := fmt.Sprintf("請開始評審 Merge Request %d。網址為：%s\n", mr.IID, mr.WebURL)
				w.SendInput(instruction)
			}
		}

		// 成功指派任務後，將該 Todo 標記為 Done，避免重複處理
		markTodoAsDone(gitlabURL, token, todo.ID)
	}
}

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
	manager := orchestrator.NewWorkerManager(cfg.Collaborators, logDir)

	fmt.Println("Starting local Workers in tmux...")
	manager.StartAll()

	fmt.Printf("Starting background Scheduler (Interval: %ds)...\n", cfg.Scheduler.IntervalSeconds)
	go func() {
		interval := cfg.Scheduler.IntervalSeconds
		if interval <= 0 {
			interval = 60
		}
		time.Sleep(15 * time.Second)

		// 讀取 Token
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Printf("[Scheduler] Error getting home directory: %v\n", err)
			return
		}
		tokenPath := filepath.Join(homeDir, ".gemini/antigravity/gitlab_token")
		tokenBytes, err := os.ReadFile(tokenPath)
		if err != nil {
			fmt.Printf("[Scheduler] Error reading GitLab token file at %s: %v\n", tokenPath, err)
			return
		}
		token := strings.TrimSpace(string(tokenBytes))

		gitlabURL := cfg.Scheduler.GitLabURL
		if gitlabURL == "" {
			gitlabURL = "https://git.efaipd.com"
		}

		username, err := getGitLabUsername(gitlabURL, token)
		if err != nil {
			fmt.Printf("[Scheduler] Warning: Error detecting GitLab username: %v\n", err)
		} else {
			fmt.Printf("[Scheduler] Detected username from token: %s\n", username)
		}

		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		for {
			scanGitLabTodos(gitlabURL, token, manager, logDir, cfg.Scheduler.AllowedProjects, cfg.Scheduler.AllowedMRAuthors)
			select {
			case <-ticker.C:
				continue
			}
		}
	}()

	fmt.Println("Local Review Monitor Mode is ACTIVE.")
	fmt.Println("Waiting for GitLab review targets...")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		answerFile := filepath.Join(logDir, "reviewer_answer.txt")
		if _, err := os.Stat(answerFile); err == nil {
			data, err := os.ReadFile(answerFile)
			if err == nil {
				content := strings.TrimSpace(string(data))
				if content != "" && !strings.Contains(content, "NO_TASKS") {
					fmt.Printf("\n==================== REVIEWER ANSWER ====================\n%s\n=========================================================\n\n", content)
					_ = os.WriteFile(answerFile, []byte(""), 0644)
				}
			}
		}
	}
}
