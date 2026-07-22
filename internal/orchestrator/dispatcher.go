package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// DispatchTaskInput 封裝發送給 Agent 的任務資訊
type DispatchTaskInput struct {
	AgentID        string
	Workspace      string
	Instruction    string
	MRIID          int
	MRWebURL       string
	CaoSessionName string
}

// TaskDispatcher 定義與 Agent 派發工具互動的介面
type TaskDispatcher interface {
	DispatchTask(ctx context.Context, input DispatchTaskInput) error
	IsBusy(ctx context.Context, agentID string) (bool, error)
	EnsureSessions(ctx context.Context, agents []CollaboratorConfig) error
	ShutdownSessions(ctx context.Context) error
}

// CaoDispatcher 實現與 cli-agent-orchestrator (cao) 的整合介面，專注於任務訊息轉發與動態 Session 管理
type CaoDispatcher struct {
	CaoBinPath  string
	SessionName string
	ServerURL   string
	HTTPClient  *http.Client
}

func NewCaoDispatcher(caoBinPath, sessionName, serverURL string) *CaoDispatcher {
	if caoBinPath == "" {
		caoBinPath = "cao"
	}
	if sessionName == "" {
		sessionName = "cao-main"
	}
	if serverURL == "" {
		serverURL = "http://localhost:9889"
	}
	return &CaoDispatcher{
		CaoBinPath:  caoBinPath,
		SessionName: sessionName,
		ServerURL:   strings.TrimSuffix(serverURL, "/"),
		HTTPClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

// normalizeProvider 將常規/常見別名規範化為 cao 支援的 Provider 名稱
func normalizeProvider(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "agy", "antigravity", "antigravity cli", "antigravity_cli":
		return "antigravity_cli"
	case "kiro", "kiro cli", "kiro_cli":
		return "kiro_cli"
	case "claude", "claude code", "claude_code":
		return "claude_code"
	case "cursor", "cursor cli", "cursor_cli":
		return "cursor_cli"
	case "copilot", "copilot cli", "copilot_cli":
		return "copilot_cli"
	default:
		return p
	}
}

// EnsureSessions 依據 config 宣告動態檢查並自動啟動對應的 CAO Sessions
func (c *CaoDispatcher) EnsureSessions(ctx context.Context, agents []CollaboratorConfig) error {
	activeOut, _ := exec.CommandContext(ctx, c.CaoBinPath, "session", "list").CombinedOutput()
	activeStr := string(activeOut)

	for _, agent := range agents {
		sessionName := agent.CaoSessionName
		if sessionName == "" {
			sessionName = fmt.Sprintf("gitlab-%s", agent.ID)
		}

		if strings.Contains(activeStr, sessionName) {
			slog.Info("CAO Session 已在運作中", "session", sessionName, "agent_id", agent.ID)
			continue
		}

		profile := agent.CaoAgentProfile
		if profile == "" {
			if agent.ID == "reviewer" {
				profile = "review_supervisor"
			} else if agent.ID == "coder" {
				profile = "code_supervisor"
			} else {
				profile = "developer"
			}
		}

		provider := normalizeProvider(agent.CaoProvider)

		slog.Info("依據設定檔動態建立與啟動 CAO Session...", "session", sessionName, "profile", profile, "provider", provider)

		args := []string{"launch", "--agents", profile, "--session-name", sessionName, "--headless", "--auto-approve"}
		if provider != "" {
			args = append(args, "--provider", provider)
		}

		cmd := exec.CommandContext(ctx, c.CaoBinPath, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Warn("動態啟動 CAO Session 失敗 (可手動啟動)", "session", sessionName, "error", err, "output", string(out))
		} else {
			slog.Info("成功依據設定檔自動建立 CAO Session", "session", sessionName)
		}
	}
	return nil
}

// ShutdownSessions 執行 CAO/tmux 的優雅關閉與清理 (獨立 Context 防護)
func (c *CaoDispatcher) ShutdownSessions(ctx context.Context) error {
	slog.Info("正在優雅關閉 CAO/tmux Sessions...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(shutdownCtx, c.CaoBinPath, "shutdown")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("執行 cao shutdown 時回傳非零狀態", "error", err, "output", string(out))
	}

	// 額外清理以 gitlab- 或 cao- 開頭的 tmux sessions 確保關閉乾淨
	cleanCmd := exec.CommandContext(shutdownCtx, "sh", "-c", "tmux list-sessions -F '#S' 2>/dev/null | grep -E '^(gitlab-|cao-)' | xargs -r -I {} tmux kill-session -t {}")
	_ = cleanCmd.Run()

	slog.Info("已成功優雅關閉所有 CAO/tmux Sessions")
	return nil
}

func (c *CaoDispatcher) DispatchTask(ctx context.Context, input DispatchTaskInput) error {
	if c.ServerURL != "" {
		err := c.dispatchViaHTTP(ctx, input)
		if err == nil {
			slog.Info("成功透過 cao-server HTTP API 送達任務訊息", "session", c.getTargetSessionName(ctx, input.CaoSessionName))
			return nil
		}
		slog.Debug("cao-server HTTP 派發未成功，轉由 CLI 模式發送", "reason", err)
	}

	return c.dispatchViaCLI(ctx, input)
}

func (c *CaoDispatcher) getTargetSessionName(ctx context.Context, requestedSession string) string {
	if requestedSession != "" {
		return requestedSession
	}
	activeSession := c.findActiveSessionName(ctx)
	if activeSession != "" {
		slog.Info("動態匹配到目前活躍中的 CAO Session", "active_session", activeSession)
		return activeSession
	}
	return c.SessionName
}

func (c *CaoDispatcher) findActiveSessionName(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "list")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 1 && strings.HasPrefix(fields[0], "cao-") {
			return fields[0]
		}
	}
	return ""
}

func (c *CaoDispatcher) dispatchViaHTTP(ctx context.Context, input DispatchTaskInput) error {
	targetSession := c.getTargetSessionName(ctx, input.CaoSessionName)
	terminalsURL := fmt.Sprintf("%s/sessions/%s/terminals", c.ServerURL, targetSession)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, terminalsURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET terminals HTTP %d", resp.StatusCode)
	}

	var terminals []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&terminals); err != nil || len(terminals) == 0 {
		return fmt.Errorf("找不到可用的 supervisor terminal id")
	}

	supervisorID := terminals[0].ID
	inputURL := fmt.Sprintf("%s/terminals/%s/input", c.ServerURL, supervisorID)
	payload := map[string]string{
		"message": input.Instruction,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, inputURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	postReq.Header.Set("Content-Type", "application/json")

	postResp, err := c.HTTPClient.Do(postReq)
	if err != nil {
		return err
	}
	defer postResp.Body.Close()

	if postResp.StatusCode >= 200 && postResp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(postResp.Body)
	return fmt.Errorf("POST input HTTP %d: %s", postResp.StatusCode, string(respBody))
}

func (c *CaoDispatcher) dispatchViaCLI(ctx context.Context, input DispatchTaskInput) error {
	targetSession := c.getTargetSessionName(ctx, input.CaoSessionName)
	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "send", targetSession, input.Instruction)
	if input.Workspace != "" {
		cmd.Dir = input.Workspace
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		slog.Info("成功透過 CLI 將任務送達 CAO Session", "session", targetSession)
		return nil
	}

	outputStr := string(out)
	if strings.Contains(outputStr, "No terminals found") || strings.Contains(outputStr, "not found") {
		return fmt.Errorf("未檢測到運作中的 CAO Session (%s)，請先在終端機執行 cao launch 啟動 Session", targetSession)
	}

	return fmt.Errorf("cao session send 失敗: %w, 輸出: %s", err, outputStr)
}

func (c *CaoDispatcher) IsBusy(ctx context.Context, agentID string) (bool, error) {
	if c.ServerURL != "" {
		busy, err := c.isBusyViaHTTP(ctx, agentID)
		if err == nil {
			return busy, nil
		}
		slog.Warn("透過 cao-server 檢查狀態失敗，降級使用 CLI 檢查", "error", err)
	}

	return c.isBusyViaCLI(ctx, agentID)
}

func (c *CaoDispatcher) isBusyViaHTTP(ctx context.Context, agentID string) (bool, error) {
	url := fmt.Sprintf("%s/sessions", c.ServerURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	return strings.Contains(string(respBody), "running"), nil
}

func (c *CaoDispatcher) isBusyViaCLI(ctx context.Context, agentID string) (bool, error) {
	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("cao session list 失敗: %w, 輸出: %s", err, string(out))
	}
	return strings.Contains(string(out), "running"), nil
}
