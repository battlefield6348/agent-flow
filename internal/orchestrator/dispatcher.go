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

// CaoDispatcher 實現與 cli-agent-orchestrator (cao) 的整合介面
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
			slog.Info("成功透過 cao-server HTTP API 派發任務", "session", c.SessionName)
			return nil
		}
		slog.Warn("透過 cao-server HTTP API 派發失敗，降級使用 CLI 執行", "error", err)
	}

	return c.dispatchViaCLI(ctx, input)
}

func (c *CaoDispatcher) dispatchViaHTTP(ctx context.Context, input DispatchTaskInput) error {
	terminalsURL := fmt.Sprintf("%s/sessions/%s/terminals", c.ServerURL, c.SessionName)
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
		return fmt.Errorf("找不到可用的 terminal id")
	}

	terminalID := terminals[0].ID
	inputURL := fmt.Sprintf("%s/terminals/%s/input", c.ServerURL, terminalID)
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
	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "send", c.SessionName, input.Instruction)
	if input.Workspace != "" {
		cmd.Dir = input.Workspace
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	outputStr := string(out)
	// 若提示找不到 Session 或 Terminal，自動呼叫 cao launch 初始化並啟動 Agent Session
	if strings.Contains(outputStr, "No terminals found") || strings.Contains(outputStr, "not found") {
		agentProfile := input.AgentID
		if agentProfile == "" {
			agentProfile = "developer"
		}
		slog.Info("偵測到 Session 未建立 Terminal，自動執行 cao launch 初始化 Session...", "session", c.SessionName, "agent_profile", agentProfile)

		launchArgs := []string{
			"launch",
			"--agents", agentProfile,
			"--session-name", c.SessionName,
			"--headless",
		}
		if input.Workspace != "" {
			launchArgs = append(launchArgs, "--working-directory", input.Workspace)
		}
		launchArgs = append(launchArgs, input.Instruction)

		launchCmd := exec.CommandContext(ctx, c.CaoBinPath, launchArgs...)
		if input.Workspace != "" {
			launchCmd.Dir = input.Workspace
		}

		launchOut, launchErr := launchCmd.CombinedOutput()
		if launchErr == nil {
			slog.Info("成功自動啟動並初始化 cao Session", "session", c.SessionName)
			return nil
		}

		return fmt.Errorf("自動啟動 cao session 失敗: %w, 輸出: %s", launchErr, string(launchOut))
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
