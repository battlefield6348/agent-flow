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

	useShell := strings.ContainsAny(cmdStr, ";&|><")

	args := []string{"new-session", "-d", "-s", sessionID}
	if workspace != "" {
		_ = os.MkdirAll(workspace, 0755)
		args = append(args, "-c", workspace)
	}
	// tmux 的 pane 程序環境繼承自 tmux「伺服器」而非 new-session 這個 client，
	// 因此 startCmd.Env 只在伺服器尚未存在（由本次啟動）時才會生效。
	// 若伺服器已由別的 collaborator 帶起，session 會繼承到對方的環境
	// （例如 GITLAB_TOKEN 拿到別的角色的 token，導致 GitLab 留言掛錯身分）。
	// 這裡用 tmux 3.2+ 的 new-session -e 逐一注入，確保每個 session 環境正確。
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	if useShell {
		args = append(args, "bash", "-c", cmdStr)
	} else {
		args = append(args, strings.Fields(cmdStr)...)
	}
	startCmd := exec.CommandContext(ctx, "tmux", args...)
	startCmd.Env = env

	if err := startCmd.Run(); err != nil {
		return err
	}

	_ = exec.Command("tmux", "set-option", "-t", sessionID, "remain-on-exit", "on").Run()
	return nil
}

func (t *TmuxTerminal) Stop(sessionID string) error {
	return exec.Command("tmux", "kill-session", "-t", sessionID).Run()
}

func (t *TmuxTerminal) SendKeys(sessionID string, keys string, enter bool) error {
	slog.Debug("Terminal SendKeys", "session_id", sessionID, "keys", keys, "enter", enter)
	err := exec.Command("tmux", "send-keys", "-t", sessionID, "-l", keys).Run()
	if err != nil {
		return err
	}
	if enter {
		time.Sleep(500 * time.Millisecond)
		return exec.Command("tmux", "send-keys", "-t", sessionID, "C-m").Run()
	}
	return nil
}

func (t *TmuxTerminal) CapturePane(sessionID string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-pt", sessionID).Output()
	return string(out), err
}

func (t *TmuxTerminal) CaptureHistory(sessionID string) ([]string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-S", "-", "-J", "-p", "-t", sessionID)
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
	checkCmd := exec.Command("tmux", "has-session", "-t", sessionID)
	return checkCmd.Run() == nil
}

func (t *TmuxTerminal) IsPaneDead(sessionID string) bool {
	cmd := exec.Command("tmux", "display-message", "-p", "-F", "#{pane_dead}", "-t", sessionID)
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == "1"
}
