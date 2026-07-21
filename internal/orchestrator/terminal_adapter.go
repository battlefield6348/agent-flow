package orchestrator

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

type TmuxTerminal struct{}

func NewTmuxTerminal() *TmuxTerminal {
	return &TmuxTerminal{}
}

func mergeTerminalEnvironment(base, overrides []string) []string {
	merged := append([]string(nil), base...)
	positions := make(map[string]int, len(merged))
	for i, item := range merged {
		if key, _, ok := strings.Cut(item, "="); ok {
			positions[key] = i
		}
	}
	for _, item := range overrides {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if i, exists := positions[key]; exists {
			merged[i] = item
			continue
		}
		positions[key] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func tmuxSessionName(id string) string {
	return "agent-" + id
}

func tmuxSessionArgs(sessionID, workspace, cmdStr string, env []string) []string {
	args := []string{"new-session", "-d", "-s", tmuxSessionName(sessionID)}
	if workspace != "" {
		args = append(args, "-c", workspace)
	}
	for _, item := range env {
		if _, _, ok := strings.Cut(item, "="); ok {
			args = append(args, "-e", item)
		}
	}
	if strings.ContainsAny(cmdStr, ";&|><") {
		return append(args, "bash", "-c", cmdStr)
	}
	return append(args, strings.Fields(cmdStr)...)
}

func (t *TmuxTerminal) Start(ctx context.Context, sessionID string, workspace string, cmdStr string, env []string) error {
	// 動態尋找絕對路徑，避免 shell 在非互動式環境中找不到命令
	parts := strings.Fields(cmdStr)
	if len(parts) > 0 {
		exe := parts[0]
		if !strings.HasPrefix(exe, "/") && !strings.HasPrefix(exe, "./") && !strings.HasPrefix(exe, "../") {
			if fullPath, err := exec.LookPath(exe); err == nil {
				parts[0] = fullPath
				cmdStr = strings.Join(parts, " ")
			}
		}
	}

	// 先殺掉舊的 session
	_ = t.Stop(sessionID)

	if workspace != "" {
		_ = os.MkdirAll(workspace, 0755)
	}
	startCmd := exec.CommandContext(ctx, "tmux", tmuxSessionArgs(sessionID, workspace, cmdStr, env)...)
	startCmd.Env = mergeTerminalEnvironment(os.Environ(), env)

	if err := startCmd.Run(); err != nil {
		return err
	}

	_ = exec.Command("tmux", "set-option", "-t", tmuxSessionName(sessionID), "remain-on-exit", "on").Run()
	return nil
}

func (t *TmuxTerminal) Stop(sessionID string) error {
	return exec.Command("tmux", "kill-session", "-t", tmuxSessionName(sessionID)).Run()
}

func (t *TmuxTerminal) SendKeys(sessionID string, keys string, enter bool) error {
	slog.Debug("Terminal SendKeys", "session_id", sessionID, "keys", keys, "enter", enter)
	err := exec.Command("tmux", "send-keys", "-t", tmuxSessionName(sessionID), "-l", keys).Run()
	if err != nil {
		return err
	}
	if enter {
		time.Sleep(500 * time.Millisecond)
		return exec.Command("tmux", "send-keys", "-t", tmuxSessionName(sessionID), "C-m").Run()
	}
	return nil
}

func (t *TmuxTerminal) CapturePane(sessionID string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-pt", tmuxSessionName(sessionID)).Output()
	return string(out), err
}

func (t *TmuxTerminal) CaptureHistory(sessionID string) ([]string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-S", "-", "-J", "-p", "-t", tmuxSessionName(sessionID))
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	rawLines := strings.Split(string(out), "\n")

	// 去除底部填充空白行
	end := len(rawLines)
	for end > 0 && strings.TrimSpace(rawLines[end-1]) == "" {
		end--
	}

	return rawLines[:end], nil
}

func (t *TmuxTerminal) HasSession(sessionID string) bool {
	checkCmd := exec.Command("tmux", "has-session", "-t", tmuxSessionName(sessionID))
	return checkCmd.Run() == nil
}

func (t *TmuxTerminal) IsPaneDead(sessionID string) bool {
	cmd := exec.Command("tmux", "display-message", "-p", "-F", "#{pane_dead}", "-t", tmuxSessionName(sessionID))
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == "1"
}
