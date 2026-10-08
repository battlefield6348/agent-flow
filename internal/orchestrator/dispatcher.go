package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	GitLabToken    string
}

// TaskDispatcher 定義與 Agent 派發工具互動的介面
type TaskDispatcher interface {
	DispatchTask(ctx context.Context, input DispatchTaskInput) error
	IsBusy(ctx context.Context, sessionName string) (bool, error)
	EnsureSessions(ctx context.Context, agents []CollaboratorConfig) error
	ShutdownSessions(ctx context.Context) error
}

// CaoDispatcher 實現與 cli-agent-orchestrator (cao) 的整合介面，專注於任務訊息轉發與動態 Session 管理
type CaoDispatcher struct {
	Mode            string
	CaoBinPath      string
	SessionName     string
	ServerURL       string
	HTTPClient      *http.Client
	CheckTmuxPrompt func(ctx context.Context, sessionName string) (ready bool, exists bool)
	LauncherFunc    func(ctx context.Context, agent CollaboratorConfig) error
	CLISendFunc     func(ctx context.Context, session, instruction, workspace string) (string, error)
	agents          []CollaboratorConfig
	mu              sync.RWMutex
}

func formatSessionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "cao-main"
	}
	if !strings.HasPrefix(name, "cao-") {
		return "cao-" + name
	}
	return name
}

func NewCaoDispatcher(caoBinPath, sessionName, serverURL string) *CaoDispatcher {
	if caoBinPath == "" {
		caoBinPath = "cao"
	}
	sessionName = formatSessionName(sessionName)
	if serverURL == "" {
		serverURL = "http://localhost:9889"
	}
	return &CaoDispatcher{
		Mode:            ModeMultiSession,
		CaoBinPath:      caoBinPath,
		SessionName:     sessionName,
		ServerURL:       strings.TrimSuffix(serverURL, "/"),
		HTTPClient:      &http.Client{Timeout: 10 * time.Second},
		CheckTmuxPrompt: isTmuxPromptReady,
	}
}

func (c *CaoDispatcher) SetMode(mode string) {
	if mode != "" {
		c.Mode = mode
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

func (c *CaoDispatcher) deleteSessionRecord(ctx context.Context, sessionName string) {
	if c.ServerURL == "" {
		return
	}
	url := fmt.Sprintf("%s/sessions/%s", c.ServerURL, sessionName)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return
	}
	resp, err := c.HTTPClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

// EnsureSessions 依據 config 宣告動態檢查並自動啟動對應的 CAO Sessions
func (c *CaoDispatcher) EnsureSessions(ctx context.Context, agents []CollaboratorConfig) error {
	c.mergeAgents(agents)

	activeOut, _ := exec.CommandContext(ctx, c.CaoBinPath, "session", "list").CombinedOutput()
	activeStr := string(activeOut)

	for _, agent := range agents {
		for _, sessionName := range agent.SessionNames() {
			replica := agent
			replica.CaoSessionName = sessionName
			if err := c.ensureSingleSession(ctx, replica, activeStr); err != nil {
				slog.Warn("動態啟動 CAO Session 發生錯誤", "agent_id", agent.ID, "session", sessionName, "error", err)
			}
		}
	}
	return nil
}

// sessionListed 以完整欄位比對 Session 名稱，避免 cao-gitlab-reviewer-1 因 cao-gitlab-reviewer-10 存在而被誤判為已啟動
func sessionListed(listOutput, sessionName string) bool {
	for _, field := range strings.Fields(listOutput) {
		if field == sessionName {
			return true
		}
	}
	return false
}

// mergeAgents 依 ID 合併代理人設定，避免單一代理人輪詢時覆蓋掉其他代理人的設定
func (c *CaoDispatcher) mergeAgents(agents []CollaboratorConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, agent := range agents {
		replaced := false
		for i := range c.agents {
			if c.agents[i].ID == agent.ID {
				c.agents[i] = agent
				replaced = true
				break
			}
		}
		if !replaced {
			c.agents = append(c.agents, agent)
		}
	}
}

func (c *CaoDispatcher) ensureSingleSession(ctx context.Context, agent CollaboratorConfig, activeStr string) error {
	if c.LauncherFunc != nil {
		return c.LauncherFunc(ctx, agent)
	}

	sessionName := agent.CaoSessionName
	if sessionName == "" {
		sessionName = fmt.Sprintf("gitlab-%s", agent.ID)
	}
	sessionName = formatSessionName(sessionName)

	if sessionListed(activeStr, sessionName) {
		slog.Info("CAO Session 已在運作中", "session", sessionName, "agent_id", agent.ID)
		return nil
	}

	c.deleteSessionRecord(ctx, sessionName)

	profile := agent.CaoAgentProfile
	if profile == "" {
		if agent.EffectiveRole() == RoleReviewer {
			profile = "review_supervisor"
		} else if agent.EffectiveRole() == RoleCoder {
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
	if agent.GitLabToken != "" {
		args = append(args,
			"--env", fmt.Sprintf("GITLAB_TOKEN=%s", agent.GitLabToken),
			"--env", fmt.Sprintf("GL_TOKEN=%s", agent.GitLabToken),
			"--env", fmt.Sprintf("GLAB_TOKEN=%s", agent.GitLabToken),
		)
	}

	cmd := exec.CommandContext(ctx, c.CaoBinPath, args...)
	if agent.GitLabToken != "" {
		env := os.Environ()
		env = append(env,
			fmt.Sprintf("GITLAB_TOKEN=%s", agent.GitLabToken),
			fmt.Sprintf("GL_TOKEN=%s", agent.GitLabToken),
			fmt.Sprintf("GLAB_TOKEN=%s", agent.GitLabToken),
		)
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("動態啟動 CAO Session 失敗 (可手動啟動)", "session", sessionName, "error", err, "output", string(out))
		return err
	}
	slog.Info("成功依據設定檔自動建立 CAO Session", "session", sessionName)
	time.Sleep(2 * time.Second)
	return nil
}

// ShutdownSessions 執行 CAO/tmux 的深層優雅關閉與清理 (同步清理 DB 紀錄與 tmux 視窗)
func (c *CaoDispatcher) ShutdownSessions(ctx context.Context) error {
	slog.Info("正在深層優雅關閉 CAO/tmux Sessions 與 DB 紀錄...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(shutdownCtx, c.CaoBinPath, "shutdown")
	_, _ = cmd.CombinedOutput()

	cleanCmd := exec.CommandContext(shutdownCtx, "sh", "-c", "tmux list-sessions -F '#S' 2>/dev/null | grep -E '^(gitlab-|cao-)' | xargs -r -I {} tmux kill-session -t {}")
	_ = cleanCmd.Run()

	slog.Info("已成功深層優雅關閉所有 CAO/tmux Sessions")
	return nil
}

func (c *CaoDispatcher) findAgentConfig(agentID, sessionName string) (CollaboratorConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, a := range c.agents {
		if agentID != "" && a.ID == agentID {
			return a, true
		}
		formattedName := formatSessionName(a.CaoSessionName)
		if sessionName != "" && (a.CaoSessionName == sessionName || formattedName == sessionName) {
			return a, true
		}
	}
	return CollaboratorConfig{}, false
}

func isSessionMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "未檢測到運作中的 cao session") ||
		strings.Contains(msg, "no terminals found") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "session 不存在")
}

func (c *CaoDispatcher) DispatchTask(ctx context.Context, input DispatchTaskInput) error {
	err := c.dispatchRaw(ctx, input)
	if err == nil {
		return nil
	}

	if isSessionMissingErr(err) {
		agentConfig, found := c.findAgentConfig(input.AgentID, input.CaoSessionName)
		if !found {
			agentConfig = CollaboratorConfig{
				ID:          input.AgentID,
				GitLabToken: input.GitLabToken,
			}
		}
		// 依 ID 找到的是整個 pool 的設定，重啟時必須針對實際派發失敗的那個 Session
		if input.CaoSessionName != "" {
			agentConfig.CaoSessionName = input.CaoSessionName
		}

		slog.Warn("檢測到 CAO Session 不存在，嘗試自動重啟中...", "agent_id", input.AgentID, "session", input.CaoSessionName)
		if relaunchErr := c.ensureSingleSession(ctx, agentConfig, ""); relaunchErr != nil {
			slog.Error("自動重啟 CAO Session 失敗", "agent_id", input.AgentID, "error", relaunchErr)
			return err
		}

		time.Sleep(1 * time.Second)
		slog.Info("Session 重啟完成，重新嘗試派發任務...", "agent_id", input.AgentID)

		retryErr := c.dispatchRaw(ctx, input)
		if retryErr == nil {
			slog.Info("自動重啟 CAO Session 後成功重新派發任務", "agent_id", input.AgentID)
			return nil
		}
		return retryErr
	}

	return err
}

func (c *CaoDispatcher) dispatchRaw(ctx context.Context, input DispatchTaskInput) error {
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
		return formatSessionName(requestedSession)
	}
	activeSession := c.findActiveSessionName(ctx)
	if activeSession != "" {
		slog.Info("動態匹配到目前活躍中的 CAO Session", "active_session", activeSession)
		return formatSessionName(activeSession)
	}
	return formatSessionName(c.SessionName)
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

	finalInstruction := input.Instruction

	payload := map[string]string{
		"message": finalInstruction,
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

type caoSessionListItem struct {
	Session   string `json:"session"`
	Conductor struct {
		ID           string `json:"id"`
		AgentProfile string `json:"agent_profile"`
		Provider     string `json:"provider"`
		Status       string `json:"status"`
	} `json:"conductor"`
}

type caoSessionDetail struct {
	Session struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"session"`
	Terminals []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"terminals"`
}

func isStatusBusy(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" || s == "completed" || s == "idle" || s == "ready" || s == "stopped" {
		return false
	}
	return true
}

func isTmuxPromptReady(ctx context.Context, sessionName string) (bool, bool) {
	if sessionName == "" {
		return false, false
	}
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", sessionName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, false
	}
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "? for shortcuts") || strings.HasPrefix(line, "────────") || strings.HasPrefix(line, "--------") {
			continue
		}
		if line == ">" || strings.HasPrefix(line, "> ") || strings.HasPrefix(line, "❯") {
			return true, true
		}
		return false, true
	}
	return false, true
}

func (c *CaoDispatcher) dispatchViaCLI(ctx context.Context, input DispatchTaskInput) error {
	targetSession := c.getTargetSessionName(ctx, input.CaoSessionName)
	finalInstruction := input.Instruction

	if c.CLISendFunc != nil {
		outStr, err := c.CLISendFunc(ctx, targetSession, finalInstruction, input.Workspace)
		if err == nil {
			slog.Info("成功透過 CLI 將任務送達 CAO Session", "session", targetSession)
			return nil
		}
		if strings.Contains(err.Error(), "未檢測到運作中的 CAO Session") {
			return err
		}
		outputStr := outStr
		if outputStr == "" {
			outputStr = err.Error()
		}
		if strings.Contains(outputStr, "No terminals found") || strings.Contains(outputStr, "not found") {
			return fmt.Errorf("未檢測到運作中的 CAO Session (%s)，請先在終端機執行 cao launch 啟動 Session", targetSession)
		}
		return err
	}

	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "send", targetSession, finalInstruction)
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
	if strings.Contains(outputStr, "is currently processing") {
		return fmt.Errorf("CAO Terminal 目前正在處理任務中 (%s): %s", targetSession, strings.TrimSpace(outputStr))
	}

	return fmt.Errorf("cao session send 失敗: %w, 輸出: %s", err, outputStr)
}

func (c *CaoDispatcher) IsBusy(ctx context.Context, sessionName string) (bool, error) {
	if c.ServerURL != "" {
		busy, err := c.isBusyViaHTTP(ctx, sessionName)
		if err == nil {
			return busy, nil
		}
		slog.Warn("透過 cao-server 檢查狀態失敗，降級使用 CLI 檢查", "error", err)
	}

	return c.isBusyViaCLI(ctx, sessionName)
}

func (c *CaoDispatcher) isBusyViaHTTP(ctx context.Context, requestedSession string) (bool, error) {
	var sessionNames []string
	if c.Mode == ModeSupervisor {
		if c.SessionName != "" {
			sessionNames = append(sessionNames, c.SessionName)
		}
		if active := c.findActiveSessionName(ctx); active != "" && active != c.SessionName {
			sessionNames = append(sessionNames, active)
		}
	} else {
		// 只檢查指定的 Session；若先檢查預設 Session，它閒置時會讓 pool 中忙碌的 Session 被誤判為閒置
		sessionNames = append(sessionNames, c.getTargetSessionName(ctx, requestedSession))
	}

	for _, sessionName := range sessionNames {
		url := fmt.Sprintf("%s/sessions/%s", c.ServerURL, sessionName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var detail caoSessionDetail
			if err := json.NewDecoder(resp.Body).Decode(&detail); err == nil {
				_ = resp.Body.Close()
				for _, term := range detail.Terminals {
					if isStatusBusy(term.Status) {
						if c.CheckTmuxPrompt != nil {
							if ready, exists := c.CheckTmuxPrompt(ctx, sessionName); exists {
								if ready {
									return false, nil
								}
								return true, nil
							}
						}
						return true, nil
					}
				}
				return false, nil
			}
		}
		_ = resp.Body.Close()
	}

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

	content := strings.ToLower(string(respBody))
	if strings.Contains(content, "running") || strings.Contains(content, "processing") || strings.Contains(content, "busy") {
		for _, sessionName := range sessionNames {
			if c.CheckTmuxPrompt != nil {
				if ready, exists := c.CheckTmuxPrompt(ctx, sessionName); exists {
					if ready {
						return false, nil
					}
					return true, nil
				}
			}
		}
		return true, nil
	}
	return false, nil
}

func (c *CaoDispatcher) isBusyViaCLI(ctx context.Context, requestedSession string) (bool, error) {
	targetSession := c.getTargetSessionName(ctx, requestedSession)

	if c.CheckTmuxPrompt != nil {
		if ready, exists := c.CheckTmuxPrompt(ctx, targetSession); exists {
			if ready {
				return false, nil
			}
			return true, nil
		}
	}

	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "list", "--json")
	out, err := cmd.CombinedOutput()
	if err == nil {
		var sessions []caoSessionListItem
		if jsonErr := json.Unmarshal(out, &sessions); jsonErr == nil {
			for _, item := range sessions {
				if targetSession != "" && (item.Session == targetSession || strings.Contains(item.Session, targetSession)) {
					if isStatusBusy(item.Conductor.Status) {
						if c.CheckTmuxPrompt != nil {
							if ready, exists := c.CheckTmuxPrompt(ctx, item.Session); exists {
								if ready {
									return false, nil
								}
								return true, nil
							}
						}
						return true, nil
					}
				}
			}
			return false, nil
		}
	}

	fallbackCmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "list")
	fallbackOut, fallbackErr := fallbackCmd.CombinedOutput()
	if fallbackErr != nil {
		return false, fmt.Errorf("cao session list 失敗: %w, 輸出: %s", fallbackErr, string(fallbackOut))
	}

	lines := strings.Split(string(fallbackOut), "\n")
	for _, line := range lines {
		if targetSession != "" && !strings.Contains(line, targetSession) {
			continue
		}
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "running") || strings.Contains(lowerLine, "processing") || strings.Contains(lowerLine, "busy") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				sessionName := fields[0]
				if c.CheckTmuxPrompt != nil {
					if ready, exists := c.CheckTmuxPrompt(ctx, sessionName); exists {
						if ready {
							return false, nil
						}
						return true, nil
					}
				}
			}
			return true, nil
		}
	}

	return false, nil
}
