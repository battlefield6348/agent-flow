package orchestrator

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Worker struct {
	Config CollaboratorConfig
	LogDir string
	cmd    *exec.Cmd // 🔴 必須保存 cmd 實例
	stopCh chan struct{}
}

func NewWorker(cfg CollaboratorConfig, logDir string) *Worker {
	return &Worker{
		Config: cfg,
		LogDir: logDir,
		stopCh: make(chan struct{}),
	}
}

func (w *Worker) Start() {
	go w.runLoop()
}

func (w *Worker) runLoop() {
	backoff := 5 * time.Second
	for {
		select {
		case <-w.stopCh:
			return
		default:
			startTime := time.Now()
			w.runProcess()
			duration := time.Since(startTime)

			// 如果處理時間太短（例如不到 10 秒就結束），可能是報錯或配額問題
			if duration < 10*time.Second {
				fmt.Printf("[%s] Process exited too quickly (%v), increasing backoff...\n", w.Config.ID, duration)
				backoff *= 2
				if backoff > 5*time.Minute {
					backoff = 5 * time.Minute
				}
			} else {
				// 正常執行完畢，重置 backoff
				backoff = 5 * time.Second
			}

			fmt.Printf("[%s] Sleeping for %v before next run...\n", w.Config.ID, backoff)
			time.Sleep(backoff)
		}
	}
}

// PrefixWriter 負責在輸出內容的每一行開頭加上指定的前綴
type PrefixWriter struct {
	ID        string
	Writer    io.Writer
	isAtStart bool // 記錄是否處於新的一行開頭
}

func (pw *PrefixWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	prefix := []byte(fmt.Sprintf("[%s] ", pw.ID))
	var out []byte

	for _, b := range p {
		if pw.isAtStart {
			out = append(out, prefix...)
			pw.isAtStart = false
		}
		out = append(out, b)
		if b == '\n' {
			pw.isAtStart = true
		}
	}

	_, err = pw.Writer.Write(out)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *Worker) runProcess() {
	sessionID := w.Config.ID

	// 1. 確保舊的 Session 已關閉
	_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()

	// 2. 啟動分離的 tmux session (不帶 prompt, 只啟動 gemini)
	fmt.Printf("[%s] Starting clean gemini session in tmux...\n", sessionID)
	// 我們透過 env 命令啟動，確保所有環境變數都被正確傳遞
	startCmd := exec.Command("tmux", "new-session", "-d", "-s", sessionID, "gemini")
	startCmd.Env = os.Environ()
	if err := startCmd.Run(); err != nil {
		fmt.Printf("[%s] Failed to start tmux session: %v\n", sessionID, err)
		return
	}

	// 3. 等待初始化並設定日誌
	time.Sleep(2 * time.Second)
	if w.LogDir != "" {
		_ = os.MkdirAll(w.LogDir, 0755)
		logPath := filepath.Join(w.LogDir, fmt.Sprintf("%s.log", sessionID))
		_ = exec.Command("tmux", "pipe-pane", "-t", sessionID, fmt.Sprintf("cat >> %s", logPath)).Run()
	}

	// 4. 如果有初始指令，透過 send-keys 「打」進去
	if w.Config.InitialInstruction != "" {
		fmt.Printf("[%s] Sending initial instruction via keys...\n", sessionID)
		// 模擬真人輸入並按下 Enter
		sendCmd := exec.Command("tmux", "send-keys", "-t", sessionID, w.Config.InitialInstruction, "Enter")
		_ = sendCmd.Run()
	}

	fmt.Printf("%s [Worker:%s] Running in tmux mode (Session: %s)\n", time.Now().Format("2006/01/02 15:04:05"), sessionID, sessionID)
	fmt.Printf("[%s] To see it work, run: tmux attach -t %s\n", sessionID, sessionID)

	// 5. 等待 session 結束
	for {
		time.Sleep(5 * time.Second)
		checkCmd := exec.Command("tmux", "has-session", "-t", sessionID)
		if err := checkCmd.Run(); err != nil {
			fmt.Printf("[%s] Session ended.\n", sessionID)
			break
		}

		select {
		case <-w.stopCh:
			return
		default:
			continue
		}
	}
}
func (w *Worker) Stop() {
	close(w.stopCh)
	sessionID := w.Config.ID
	fmt.Printf("Killing tmux session for %s...\n", sessionID)
	_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()
}

type WorkerManager struct {
	Workers []*Worker
}

func NewWorkerManager(configs []CollaboratorConfig, logDir string) *WorkerManager {
	var workers []*Worker
	for _, cfg := range configs {
		workers = append(workers, NewWorker(cfg, logDir))
	}
	return &WorkerManager{Workers: workers}
}

func (m *WorkerManager) StartAll() {
	for i, w := range m.Workers {
		if i > 0 {
			fmt.Printf("[Orchestrator] Waiting 60s before starting next worker (%s) to avoid quota burst...\n", w.Config.ID)
			time.Sleep(60 * time.Second)
		}
		w.Start()
	}
}

func (m *WorkerManager) StopAll() {
	for _, w := range m.Workers {
		w.Stop()
	}
}
