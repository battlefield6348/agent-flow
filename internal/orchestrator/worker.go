package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Worker struct {
	Config         CollaboratorConfig
	LogDir         string
	Terminal       Terminal
	stopCh         chan struct{}
	inputCh        chan string
	outputCallback func(string)
	lastOutput     string
	muLast         sync.Mutex
	isBusy         bool
	muBusy         sync.Mutex
}

// 判定此 Worker 是否正在執行對話任務中
func (w *Worker) IsBusy() bool {
	w.muBusy.Lock()
	defer w.muBusy.Unlock()
	return w.isBusy
}

// 設定此 Worker 的忙碌狀態
func (w *Worker) setBusy(busy bool) {
	w.muBusy.Lock()
	w.isBusy = busy
	w.muBusy.Unlock()
}

func (w *Worker) SetOutputCallback(cb func(string)) {
	w.outputCallback = cb
}

func (w *Worker) IsRunning() bool {
	if !w.Terminal.HasSession(w.Config.ID) {
		return false
	}
	return !w.Terminal.IsPaneDead(w.Config.ID)
}

func NewWorker(cfg CollaboratorConfig, logDir string, terminal Terminal) *Worker {
	return &Worker{
		Config:   cfg,
		LogDir:   logDir,
		Terminal: terminal,
		stopCh:   make(chan struct{}),
		inputCh:  make(chan string, 10),
	}
}

func (w *Worker) SendInput(text string) {
	if w.Config.InputPrefix != "" {
		text = w.Config.InputPrefix + text
	}
	w.inputCh <- text
}

func (w *Worker) Start() {
	w.stopCh = make(chan struct{})
	go w.runLoop()
}

func (w *Worker) runLoop() {
	for {
		select {
		case <-w.stopCh:
			return
		default:
			w.runProcess()
			time.Sleep(5 * time.Second)
		}
	}
}

// isPromptReady 檢查畫面最後幾行是否出現可輸入的提示，避免歷史中的舊提示符造成誤判
func (w *Worker) isPromptReady(screen string) bool {
	rawLines := strings.Split(screen, "\n")
	if len(rawLines) == 0 {
		return false
	}

	// 排除 tmux 視窗底部填充的空白空行，精確定位有內容的最後幾行
	end := len(rawLines)
	for end > 0 && strings.TrimSpace(rawLines[end-1]) == "" {
		end--
	}
	if end == 0 {
		return false
	}

	start := end - 3
	if start < 0 {
		start = 0
	}
	lastRows := rawLines[start:end]

	// 檢查最後幾行是否有 thinking 等關鍵字，有的話說明還在處理中
	for _, row := range lastRows {
		lower := strings.ToLower(row)
		if strings.Contains(lower, "thinking") || strings.Contains(lower, "queued") || strings.Contains(lower, "working") {
			return false
		}
	}

	// 檢查最後幾行是否包含提示字元
	prompts := []string{
		">",
		"›",
		"»",
		"Type your message",
		"workspace (",
		"shift+tab",
		"gpt-5.3-codex",
	}

	for _, row := range lastRows {
		for _, p := range prompts {
			if strings.Contains(row, p) {
				return true
			}
		}
	}
	return false
}

func (w *Worker) runProcess() {
	sessionID := w.Config.ID
	slog.Info("Worker engine starting", "worker_id", sessionID)

	var additionalArgs []string
	homeDir, err := os.UserHomeDir()
	if err == nil {
		for _, skill := range w.Config.Skills {
			skillPath := filepath.Join(homeDir, ".gemini/antigravity/skills", skill)
			if _, err := os.Stat(skillPath); err == nil {
				additionalArgs = append(additionalArgs, "--add-dir", skillPath)
			}
		}
	}

	allArgs := append(w.Config.Args, additionalArgs...)
	argsStr := strings.TrimSpace(strings.Join(allArgs, " "))
	var fullCmd string
	if argsStr != "" {
		fullCmd = fmt.Sprintf("%s %s", w.Config.Cmd, argsStr)
	} else {
		fullCmd = w.Config.Cmd
	}

	if err := w.Terminal.Start(context.Background(), sessionID, w.Config.Workspace, fullCmd, os.Environ()); err != nil {
		slog.Error("Failed to start terminal", "worker_id", sessionID, "error", err)
		return
	}

	slog.Debug("Waiting for CLI initialization", "worker_id", sessionID)
	ready := false
	for i := 0; i < 45; i++ {
		time.Sleep(2 * time.Second)
		screen, _ := w.Terminal.CapturePane(sessionID)
		if w.isPromptReady(screen) {
			slog.Info("Worker CLI is READY", "worker_id", sessionID)
			ready = true
			break
		}
	}

	if !ready {
		slog.Warn("Ready pattern not detected, proceeding anyway", "worker_id", sessionID)
	}

	if ready && len(w.Config.Skills) > 0 {
		for _, skill := range w.Config.Skills {
			slog.Info("Injecting skill", "worker_id", sessionID, "skill", skill)
			skillCmd := fmt.Sprintf("/superpowers:%s 請待命，等候我給予你具體的 Merge Request 評審任務。", skill)
			_ = w.Terminal.SendKeys(sessionID, skillCmd, true)

			time.Sleep(5 * time.Second)
			for i := 0; i < 45; i++ {
				screen, _ := w.Terminal.CapturePane(sessionID)
				if w.isPromptReady(screen) {
					break
				}
				time.Sleep(2 * time.Second)
			}
		}
	}

	stopInput := make(chan struct{})
	go func() {
		for {
			select {
			case input := <-w.inputCh:
				w.handleInput(input, sessionID)
			case <-stopInput:
				return
			case <-w.stopCh:
				return
			}
		}
	}()

	for {
		time.Sleep(10 * time.Second)
		if !w.IsRunning() {
			screen, _ := w.Terminal.CapturePane(sessionID)
			slog.Error("Worker session DIED", "worker_id", sessionID, "last_screen", screen)

			slog.Info("Cleaning up died session", "worker_id", sessionID)
			close(stopInput)
			break
		}
		select {
		case <-w.stopCh:
			close(stopInput)
			return
		default:
		}
	}
}

// 處理來自通道的評審或開發指令，並追蹤其忙碌狀態與結果輸出
func (w *Worker) handleInput(input string, sessionID string) {
	w.setBusy(true)
	defer w.setBusy(false)

	slog.Info("Forwarding input to worker", "worker_id", sessionID, "input", input)

	// 等待 AI 進入就緒狀態
	for i := 0; i < 45; i++ {
		screen, _ := w.Terminal.CapturePane(sessionID)
		if w.isPromptReady(screen) {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// 記錄發送問題前的終端歷史
	linesBefore, err := w.Terminal.CaptureHistory(sessionID)
	var N int
	if err == nil {
		N = len(linesBefore)
	}

	// 將輸入傳送到終端
	_ = w.Terminal.SendKeys(sessionID, input, true)
	_ = w.Terminal.SendKeys(sessionID, "", true) // 多按一個 Enter 確保觸發

	time.Sleep(2 * time.Second)

	// 持續偵測內容變化，等待提示符重新出現
	maxPolls := 3600
	var lastScreen string
	sameCount := 0
	for poll := 0; poll < maxPolls; poll++ {
		time.Sleep(1 * time.Second)
		if w.Terminal.IsPaneDead(sessionID) {
			slog.Warn("Detected pane dead during polling", "worker_id", sessionID)
			break
		}
		currScreen, _ := w.Terminal.CapturePane(sessionID)

		currClean := CleanLine(currScreen)
		lastClean := CleanLine(lastScreen)

		if currClean == lastClean && currClean != "" {
			sameCount++
		} else {
			sameCount = 0
		}
		lastScreen = currScreen

		if sameCount >= 2 {
			if w.isPromptReady(currScreen) {
				break
			}
		}
	}

	// 抓取執行完畢後的完整歷史
	linesAfter, err := w.Terminal.CaptureHistory(sessionID)
	if err != nil {
		slog.Error("Error getting terminal history", "worker_id", sessionID, "error", err)
		return
	}

	var newLines []string
	if len(linesAfter) > N {
		newLines = linesAfter[N:]
	} else {
		newLines = linesAfter
	}

	// 過濾與清理每一行
	var cleanLines []string
	for _, line := range newLines {
		cleaned := CleanLine(line)
		if ShouldIgnore(cleaned) {
			continue
		}

		trimmedLine := strings.TrimSpace(cleaned)
		for _, p := range []string{">", "›", "»", "🤖"} {
			trimmedLine = strings.TrimPrefix(trimmedLine, p)
			trimmedLine = strings.TrimSpace(trimmedLine)
		}
		if trimmedLine == strings.TrimSpace(input) {
			continue
		}

		if trimmedLine == "" {
			continue
		}

		cleanLines = append(cleanLines, cleaned)
	}

	// 拼接並進一步清理完整回覆
	fullText := strings.TrimSpace(strings.Join(cleanLines, "\n"))
	fullText = CleanBlock(fullText)

	if w.Config.OnlyFinalResponse || w.Config.ID == "reviewer" || w.Config.ID == "coder" {
		fullText = ParseFinalResponse(fullText)
	}

	w.muLast.Lock()
	last := w.lastOutput
	w.muLast.Unlock()

	if fullText != "" && fullText != strings.TrimSpace(last) {
		_ = os.MkdirAll(w.LogDir, 0755)
		answerFile := filepath.Join(w.LogDir, fmt.Sprintf("%s_answer.txt", sessionID))
		if err := os.WriteFile(answerFile, []byte(fullText), 0644); err != nil {
			slog.Error("Error writing answer file", "worker_id", sessionID, "error", err)
		} else {
			slog.Info("Wrote answer to file", "worker_id", sessionID, "bytes", len(fullText))
		}

		if w.outputCallback != nil {
			w.outputCallback(fullText)
		}
		w.muLast.Lock()
		w.lastOutput = fullText
		w.muLast.Unlock()
	}
}

func (w *Worker) Stop() {
	close(w.stopCh)
	sessionID := w.Config.ID
	slog.Info("Stopping worker terminal session", "worker_id", sessionID)
	_ = w.Terminal.Stop(sessionID)
}

type WorkerManager struct {
	Workers []*Worker
}

func NewWorkerManager(configs []CollaboratorConfig, logDir string, terminal Terminal) *WorkerManager {
	var workers []*Worker
	for _, cfg := range configs {
		workers = append(workers, NewWorker(cfg, logDir, terminal))
	}
	return &WorkerManager{Workers: workers}
}

func (m *WorkerManager) StartAll() {
	for i, w := range m.Workers {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		w.Start()
	}
}

func (m *WorkerManager) StopAll() {
	for _, w := range m.Workers {
		w.Stop()
	}
}
