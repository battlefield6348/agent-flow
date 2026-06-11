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
	Reviewers   []struct {
		Username string `json:"username"`
	} `json:"reviewers"`
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

func scanGitLabMRs(gitlabURL, token, username string, manager *orchestrator.WorkerManager, processedMRs map[int]string) {
	apiURL := fmt.Sprintf("%s/api/v4/merge_requests?state=opened&scope=all&order_by=updated_at&sort=desc&per_page=100", gitlabURL)

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

	var mrs []GitLabMR
	if err := json.NewDecoder(resp.Body).Decode(&mrs); err != nil {
		fmt.Printf("[Scheduler] Error parsing MR JSON: %v\n", err)
		return
	}

	for _, mr := range mrs {
		isTagged := false
		descLower := strings.ToLower(mr.Description)
		titleLower := strings.ToLower(mr.Title)
		if strings.Contains(descLower, "#reviewer") || strings.Contains(titleLower, "#reviewer") {
			isTagged = true
		}
		if username != "" {
			for _, r := range mr.Reviewers {
				if strings.ToLower(r.Username) == strings.ToLower(username) {
					isTagged = true
					break
				}
			}
		}

		if isTagged {
			lastSHA, exists := processedMRs[mr.IID]
			if !exists || lastSHA != mr.SHA {
				processedMRs[mr.IID] = mr.SHA
				fmt.Printf("[Scheduler] Found review target: MR %d (%s), resolving local workspace...\n", mr.IID, mr.Title)

				projectPath, err := getProjectPathFromWebURL(mr.WebURL)
				if err != nil {
					fmt.Printf("[Scheduler] Error parsing project path from URL: %v\n", err)
					continue
				}

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
			}
		}
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

	if cfg.Scheduler.Enable {
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

			processedMRs := make(map[int]string)
			ticker := time.NewTicker(time.Duration(interval) * time.Second)
			defer ticker.Stop()
			for {
				scanGitLabMRs(gitlabURL, token, username, manager, processedMRs)
				select {
				case <-ticker.C:
					continue
				}
			}
		}()
	}

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
