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
	AgentID     string
	Workspace   string
	Instruction string
	MRIID       int
	MRWebURL    string
}

// TaskDispatcher 定義與 Agent 派發工具互動的介面
type TaskDispatcher interface {
	DispatchTask(ctx context.Context, input DispatchTaskInput) error
	IsBusy(ctx context.Context, agentID string) (bool, error)
}

// CaoDispatcher 實現與 cli-agent-orchestrator (cao) 的整合介面，專注於任務訊息轉發
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

func (c *CaoDispatcher) DispatchTask(ctx context.Context, input DispatchTaskInput) error {
	if c.ServerURL != "" {
		err := c.dispatchViaHTTP(ctx, input)
		if err == nil {
			slog.Info("成功透過 cao-server HTTP API 送達任務訊息", "session", c.SessionName)
			return nil
		}
		slog.Debug("cao-server HTTP 派發未成功，轉由 CLI 模式發送", "reason", err)
	}

	return c.dispatchViaCLI(ctx, input)
}

func (c *CaoDispatcher) getTargetSessionName(ctx context.Context) string {
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
	targetSession := c.getTargetSessionName(ctx)
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
	targetSession := c.getTargetSessionName(ctx)
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
