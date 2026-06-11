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
	"gemini-collaborator-go/internal/telegram"
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

func getGitLabProjectPath() (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	urlStr := strings.TrimSpace(string(out))
	urlStr = strings.TrimSuffix(urlStr, ".git")

	if strings.HasPrefix(urlStr, "git@") {
		parts := strings.SplitN(urlStr, ":", 2)
		if len(parts) == 2 {
			return parts[1], nil
		}
	}

	if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
		parsed, err := url.Parse(urlStr)
		if err != nil {
			return "", err
		}
		return strings.TrimPrefix(parsed.Path, "/"), nil
	}

	return "", fmt.Errorf("unsupported remote URL: %s", urlStr)
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

func scanGitLabMRs(gitlabURL, token, projectPath, username string, manager *orchestrator.WorkerManager, processedMRs map[int]string) {
	projectPathEscaped := url.PathEscape(projectPath)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests?state=opened", gitlabURL, projectPathEscaped)

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
				fmt.Printf("[Scheduler] Found review target: MR %d (%s), triggering Reviewer...\n", mr.IID, mr.Title)

				for _, w := range manager.Workers {
					if w.Config.ID == "reviewer" {
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

	ensureMCPRegistered(cfg.Database.Path)

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

			// 自動探測專案與使用者
			projectPath, err := getGitLabProjectPath()
			if err != nil {
				fmt.Printf("[Scheduler] Error detecting GitLab project path: %v\n", err)
				return
			}
			fmt.Printf("[Scheduler] Detected project path: %s\n", projectPath)

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
				scanGitLabMRs(gitlabURL, token, projectPath, username, manager, processedMRs)
				select {
				case <-ticker.C:
					continue
				}
			}
		}()
	}

	bot, err := telegram.NewBot(cfg.Telegram.Token, cfg.Telegram.AllowedChatIDs, cfg.Collaborators, manager)
	if err != nil {
		fmt.Printf("Failed to initialize Telegram Bot: %v\n", err)
		manager.StopAll()
		os.Exit(1)
	}

	fmt.Println("Telegram War Room Mode is ACTIVE.")
	fmt.Println("Connect to your Telegram group and start collaborating!")

	bot.Start()
}

func ensureMCPRegistered(dbPath string) {
	serverPath := "./mcp-server"
	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		fmt.Println("Warning: mcp-server binary not found. Please run 'make build' first.")
		return
	}

	absServer, _ := filepath.Abs(serverPath)
	absDB, _ := filepath.Abs(dbPath)

	// 註冊或更新 MCP Server 設定
	fmt.Printf("Ensuring MCP server 'collaborator-tools' is registered (DB: %s)...\n", absDB)
	cmd := exec.Command("gemini", "mcp", "add", "collaborator-tools", absServer, "--db", absDB)
	if err := cmd.Run(); err != nil {
		fmt.Printf("Note: MCP registration might have skipped or failed: %v\n", err)
	}
}
