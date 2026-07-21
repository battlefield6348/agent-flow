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
	running        bool
	muRun          sync.Mutex
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

func (w *Worker) BuildPromptMsg(sessionID string) string {
	var promptMsg string
	switch sessionID {
	case "coder":
		promptMsg = "請待命，等候我給予你具體的開發與修正任務。"
	case "reviewer":
		promptMsg = "請待命，等候我給予你具體的 Merge Request 評審任務。"
	default:
		promptMsg = "請待命，等候我給予你具體的任務。"
	}
	if w.Config.PromptSuffix != "" {
		promptMsg += w.Config.PromptSuffix
	}
	return promptMsg
}

func (w *Worker) SendInput(text string) {
	if w.Config.InputPrefix != "" {
		text = w.Config.InputPrefix + text
	}
	w.inputCh <- text
}

func (w *Worker) Start() {
	w.muRun.Lock()
	defer w.muRun.Unlock()
	if w.running {
		slog.Warn("Worker is already running", "worker_id", w.Config.ID)
		return
	}
	w.stopCh = make(chan struct{})
	w.running = true
	go w.runLoop(w.stopCh)
}

func (w *Worker) runLoop(stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		default:
			w.runProcess(stopCh)
			time.Sleep(5 * time.Second)
		}
	}
}

// isPromptReady 檢查畫面最後幾行是否出現可輸入的提示，避免歷史中的舊提示符或標題列造成誤判
func (w *Worker) isPromptReady(screen string) bool {
	rawLines := strings.Split(screen, "\n")
	if len(rawLines) == 0 {
		return false
	}

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

	for _, row := range lastRows {
		lower := strings.ToLower(row)
		if strings.Contains(lower, "thinking") || strings.Contains(lower, "queued") || strings.Contains(lower, "working") {
			return false
		}
	}

	var prompts []string
	cmdLower := strings.ToLower(w.Config.Cmd)
	if w.Config.ID == "reviewer" || strings.Contains(cmdLower, "agy") || strings.Contains(cmdLower, "antigravity") {
		prompts = []string{">", "»", "Type your message"}
	} else {
		prompts = []string{"›", "Type your message", "workspace (", "shift+tab"}
	}

	for _, row := range lastRows {
		if strings.Contains(row, "OpenAI Codex") || strings.Contains(row, "───") {
			continue
		}
		for _, p := range prompts {
			if strings.Contains(row, p) {
				return true
			}
		}
	}
	return false
}

func (w *Worker) runProcess(stopCh <-chan struct{}) {
	sessionID := w.Config.ID
	slog.Info("Worker engine starting", "worker_id", sessionID)

	skills := w.Config.Skills
	if len(skills) == 0 {
		skills = DefaultSkills(w.Config.ID)
	}
	additionalArgs := make([]string, 0, len(skills))
	loadedSkills := make([]string, 0, len(skills))
	if homeDir, err := os.UserHomeDir(); err == nil {
		for _, skill := range skills {
			skillName := strings.TrimPrefix(skill, "superpowers:")
			if _, err := os.Stat(filepath.Join(homeDir, ".gemini/antigravity/skills", skillName)); err != nil {
				continue
			}
			if !strings.Contains(strings.ToLower(w.Config.Cmd), "agy") {
				additionalArgs = append(additionalArgs, "--add-dir", filepath.Join(homeDir, ".gemini/antigravity/skills", skillName))
			}
			loadedSkills = append(loadedSkills, skillName)
		}
	}

	args := append(append([]string(nil), w.Config.Args...), additionalArgs...)
	argsStr := strings.TrimSpace(strings.Join(args, " "))
	var fullCmd string
	if argsStr != "" {
		fullCmd = fmt.Sprintf("%s %s", w.Config.Cmd, argsStr)
	} else {
		fullCmd = w.Config.Cmd
	}

	env := []string{"TERM=screen-256color"}
	for _, pair := range []struct{ key, value string }{
		{"GITLAB_TOKEN", w.Config.GitLabToken},
	} {
		if pair.value != "" {
			env = append(env, pair.key+"="+pair.value)
		}
	}

	if err := w.Terminal.Start(context.Background(), sessionID, w.Config.Workspace, fullCmd, env); err != nil {
		slog.Error("Failed to start terminal", "worker_id", sessionID, "error", err)
		return
	}

	slog.Debug("Waiting for CLI initialization", "worker_id", sessionID)
	ready := false
	for i := 0; i < 45; i++ {
		time.Sleep(2 * time.Second)
		screen, _ := w.Terminal.CapturePane(sessionID)
		if strings.Contains(screen, "Do you trust the contents of this project?") {
			slog.Info("Trust prompt detected, sending Enter to confirm", "worker_id", sessionID)
			_ = w.Terminal.SendKeys(sessionID, "", true)
			time.Sleep(2 * time.Second)
			continue
		}
		if w.isPromptReady(screen) {
			slog.Info("Worker CLI is READY", "worker_id", sessionID)
			ready = true
			break
		}
	}

	if !ready {
		slog.Warn("Ready pattern not detected, proceeding anyway", "worker_id", sessionID)
	}
	if ready {
		for _, skill := range loadedSkills {
			_ = w.Terminal.SendKeys(sessionID, fmt.Sprintf("%s%s %s", w.GetSkillPrefix(), skill, w.BuildPromptMsg(sessionID)), true)
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
			case <-stopCh:
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
		case <-stopCh:
			close(stopInput)
			return
		default:
		}
	}
}

// handleInput 處理來自通道的指令，並追蹤其忙碌狀態與結果輸出
func (w *Worker) handleInput(input string, sessionID string) {
	w.setBusy(true)
	defer w.setBusy(false)

	slog.Info("Forwarding input to worker", "worker_id", sessionID, "input", input)

	if !w.waitForReady(45) {
		slog.Warn("Worker not ready for input, proceeding anyway", "worker_id", sessionID)
	}

	// 記錄發送指令前的畫面與歷史，以避免把舊提示符誤判為新任務完成。
	screenBefore, _ := w.Terminal.CapturePane(sessionID)
	nBefore := w.getHistoryLineCount(sessionID)

	_ = w.Terminal.SendKeys(sessionID, input, true)
	_ = w.Terminal.SendKeys(sessionID, "", true) // 額外的 Enter 確保 CLI 觸發執行

	// 等待執行完成並提取輸出
	if !w.pollUntilReady(3600, screenBefore) {
		slog.Warn("Polling timeout or interrupted", "worker_id", sessionID)
	}

	w.processAndSaveOutput(sessionID, nBefore, input)
}

// waitForReady 等待終端出現可輸入提示
func (w *Worker) waitForReady(maxAttempts int) bool {
	for i := 0; i < maxAttempts; i++ {
		screen, _ := w.Terminal.CapturePane(w.Config.ID)
		if w.isPromptReady(screen) {
			return true
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// pollUntilReady 持續偵測內容變化，直到畫面穩定且出現提示符
func taskCompletionReady(seenChange bool, sameCount int, screen string) bool {
	return seenChange && sameCount >= 2 && strings.TrimSpace(screen) != ""
}

func (w *Worker) pollUntilReady(maxSeconds int, beforeScreen string) bool {
	lastScreen := beforeScreen
	sameCount := 0
	seenChange := false
	for poll := 0; poll < maxSeconds; poll++ {
		time.Sleep(1 * time.Second)
		if w.Terminal.IsPaneDead(w.Config.ID) {
			return false
		}
		currScreen, _ := w.Terminal.CapturePane(w.Config.ID)

		// 透過清理後的文字比對來判定畫面是否停止更新
		if CleanLine(currScreen) == CleanLine(lastScreen) && currScreen != "" {
			sameCount++
		} else {
			if CleanLine(currScreen) != CleanLine(beforeScreen) {
				seenChange = true
			}
			sameCount = 0
		}
		lastScreen = currScreen

		// 必須先看見新任務造成畫面變化，才可將穩定提示符視為完成。
		if taskCompletionReady(seenChange, sameCount, currScreen) && w.isPromptReady(currScreen) {
			return true
		}
	}
	return false
}

// getHistoryLineCount 取得當前終端歷史的總行數
func (w *Worker) getHistoryLineCount(sessionID string) int {
	lines, err := w.Terminal.CaptureHistory(sessionID)
	if err != nil {
		return 0
	}
	return len(lines)
}

// processAndSaveOutput 提取增量歷史，進行清理過濾後保存至檔案並觸發回調
func (w *Worker) processAndSaveOutput(sessionID string, nBefore int, originalInput string) {
	linesAfter, err := w.Terminal.CaptureHistory(sessionID)
	if err != nil {
		slog.Error("Error getting terminal history", "worker_id", sessionID, "error", err)
		return
	}

	var newLines []string
	if len(linesAfter) > nBefore {
		newLines = linesAfter[nBefore:]
	} else {
		newLines = linesAfter
	}

	fullText := w.filterAndJoinLines(newLines, originalInput)
	if fullText == "" {
		return
	}

	w.muLast.Lock()
	isDuplicate := fullText == strings.TrimSpace(w.lastOutput)
	w.muLast.Unlock()

	if !isDuplicate {
		w.saveAnswerToFile(sessionID, fullText)
		if w.outputCallback != nil {
			w.outputCallback(fullText)
		}
		w.muLast.Lock()
		w.lastOutput = fullText
		w.muLast.Unlock()
	}
}

// filterAndJoinLines 清理增量行並拼接成最終文本
func (w *Worker) filterAndJoinLines(lines []string, originalInput string) string {
	var cleanLines []string
	inputTrimmed := strings.TrimSpace(originalInput)

	for _, line := range lines {
		cleaned := CleanLine(line)
		if ShouldIgnore(cleaned) {
			continue
		}

		// 移除常見的提示符前綴
		trimmedLine := strings.TrimSpace(cleaned)
		for _, p := range []string{">", "›", "»", "🤖"} {
			trimmedLine = strings.TrimPrefix(trimmedLine, p)
			trimmedLine = strings.TrimSpace(trimmedLine)
		}

		// 過濾掉輸入內容本身，僅保留 AI 回覆
		if trimmedLine == inputTrimmed || trimmedLine == "" {
			continue
		}
		cleanLines = append(cleanLines, cleaned)
	}

	fullText := strings.TrimSpace(strings.Join(cleanLines, "\n"))
	fullText = CleanBlock(fullText)

	if w.Config.OnlyFinalResponse || w.Config.ID == "reviewer" || w.Config.ID == "coder" {
		fullText = ParseFinalResponse(fullText)
	}
	return fullText
}

func (w *Worker) saveAnswerToFile(sessionID, content string) {
	_ = os.MkdirAll(w.LogDir, 0755)
	answerFile := filepath.Join(w.LogDir, fmt.Sprintf("%s_answer.txt", sessionID))
	if err := os.WriteFile(answerFile, []byte(content), 0644); err != nil {
		slog.Error("Error writing answer file", "worker_id", sessionID, "error", err)
	} else {
		slog.Info("Wrote answer to file", "worker_id", sessionID, "bytes", len(content))
	}
}

func (w *Worker) Stop() {
	w.muRun.Lock()
	defer w.muRun.Unlock()
	if !w.running {
		slog.Warn("Worker is not running", "worker_id", w.Config.ID)
		return
	}
	close(w.stopCh)
	w.running = false
	sessionID := w.Config.ID
	slog.Info("Stopping worker terminal session", "worker_id", sessionID)
	_ = w.Terminal.Stop(sessionID)
}

// GetSkillPrefix 依據執行命令與協作者 ID 決定技能前綴字元。
// 為了避免多平台指令混淆，針對 agy/antigravity 體系使用 '/'，對 codex 體系使用 '$'。
func (w *Worker) GetSkillPrefix() string {
	prefix := "$"
	cmdLower := strings.ToLower(w.Config.Cmd)
	if w.Config.ID == "reviewer" || strings.Contains(cmdLower, "agy") || strings.Contains(cmdLower, "antigravity") {
		prefix = "/"
	} else if w.Config.ID == "coder" || strings.Contains(cmdLower, "codex") {
		prefix = "$"
	}
	return prefix
}

type WorkerManager struct {
	Workers  []*Worker
	logDir   string
	terminal Terminal
	mu       sync.RWMutex
}

func NewWorkerManager(configs []CollaboratorConfig, logDir string, terminal Terminal) *WorkerManager {
	var workers []*Worker
	for _, cfg := range configs {
		workers = append(workers, NewWorker(cfg, logDir, terminal))
	}
	return &WorkerManager{Workers: workers, logDir: logDir, terminal: terminal}
}

type WorkerStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Cmd       string `json:"cmd"`
	Workspace string `json:"workspace"`
	Running   bool   `json:"running"`
	Busy      bool   `json:"busy"`
}

func (m *WorkerManager) AddAndStart(cfg CollaboratorConfig) error {
	m.mu.Lock()
	for _, worker := range m.Workers {
		if worker.Config.ID == cfg.ID {
			m.mu.Unlock()
			return fmt.Errorf("worker %s already exists", cfg.ID)
		}
	}
	worker := NewWorker(cfg, m.logDir, m.terminal)
	m.Workers = append(m.Workers, worker)
	m.mu.Unlock()
	worker.Start()
	return nil
}

func (m *WorkerManager) Statuses() []WorkerStatus {
	m.mu.RLock()
	workers := append([]*Worker(nil), m.Workers...)
	m.mu.RUnlock()
	statuses := make([]WorkerStatus, 0, len(workers))
	for _, worker := range workers {
		statuses = append(statuses, WorkerStatus{ID: worker.Config.ID, Name: worker.Config.Name, Cmd: worker.Config.Cmd, Workspace: worker.Config.Workspace, Running: worker.IsRunning(), Busy: worker.IsBusy()})
	}
	return statuses
}

func (m *WorkerManager) Find(id string) *Worker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, worker := range m.Workers {
		if worker.Config.ID == id {
			return worker
		}
	}
	return nil
}

func (m *WorkerManager) Restart(id string) error {
	worker := m.Find(id)
	if worker == nil {
		return fmt.Errorf("worker %s does not exist", id)
	}
	worker.Stop()
	worker.Start()
	return nil
}

func (m *WorkerManager) StartAll() {
	m.mu.RLock()
	workers := append([]*Worker(nil), m.Workers...)
	m.mu.RUnlock()
	for i, w := range workers {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		w.Start()
	}
}

func (m *WorkerManager) StopAll() {
	m.mu.RLock()
	workers := append([]*Worker(nil), m.Workers...)
	m.mu.RUnlock()
	for _, w := range workers {
		w.Stop()
	}
}
