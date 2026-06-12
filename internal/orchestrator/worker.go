package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Worker struct {
	Config         CollaboratorConfig
	LogDir         string
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
	checkCmd := exec.Command("tmux", "has-session", "-t", w.Config.ID)
	if err := checkCmd.Run(); err != nil {
		return false
	}
	return !w.isPaneDead(w.Config.ID)
}

func (w *Worker) isPaneDead(sessionID string) bool {
	cmd := exec.Command("tmux", "display-message", "-p", "-F", "#{pane_dead}", "-t", sessionID)
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == "1"
}

func NewWorker(cfg CollaboratorConfig, logDir string) *Worker {
	return &Worker{
		Config:  cfg,
		LogDir:  logDir,
		stopCh:  make(chan struct{}),
		inputCh: make(chan string, 10),
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

func (w *Worker) getHistoryLines(sessionID string) ([]string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-S", "-", "-J", "-p", "-t", sessionID)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	rawLines := strings.Split(string(out), "\n")

	// 去除 tmux 視窗底部填充的空白行，以取得精確的實際內容邊界
	end := len(rawLines)
	for end > 0 && strings.TrimSpace(rawLines[end-1]) == "" {
		end--
	}

	var lines []string
	for i := 0; i < end; i++ {
		lines = append(lines, rawLines[i])
	}
	return lines, nil
}

func (w *Worker) runProcess() {
	sessionID := w.Config.ID
	_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()

	fmt.Printf("[%s] Engine started in tmux (PTY) mode.\n", sessionID)

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

	var startCmd *exec.Cmd
	if w.Config.Workspace != "" {
		// 確保指定的工作目錄存在，避免 tmux 啟動失敗
		_ = os.MkdirAll(w.Config.Workspace, 0755)
		startCmd = exec.Command("tmux", "new-session", "-d", "-s", sessionID, "-c", w.Config.Workspace, "sh", "-c", fullCmd)
	} else {
		startCmd = exec.Command("tmux", "new-session", "-d", "-s", sessionID, "sh", "-c", fullCmd)
	}
	startCmd.Env = os.Environ()

	if err := startCmd.Run(); err != nil {
		fmt.Printf("[%s] CRITICAL: Failed to run tmux start: %v\n", sessionID, err)
		return
	}
	_ = exec.Command("tmux", "set-option", "-t", sessionID, "remain-on-exit", "on").Run()

	fmt.Printf("[%s] Waiting for CLI initialization...\n", sessionID)
	ready := false
	for i := 0; i < 45; i++ {
		time.Sleep(2 * time.Second)
		checkCmd := exec.Command("tmux", "capture-pane", "-pt", sessionID)
		out, _ := checkCmd.Output()
		if w.isPromptReady(string(out)) {
			fmt.Printf("[%s] CLI is READY.\n", sessionID)
			ready = true
			break
		}
	}

	if !ready {
		fmt.Printf("[%s] WARNING: Ready pattern not detected, proceeding anyway...\n", sessionID)
	}

	if ready && len(w.Config.Skills) > 0 {
		for _, skill := range w.Config.Skills {
			fmt.Printf("[%s] Injecting skill with standby prompt: %s\n", sessionID, skill)
			// 將待命指令直接附加於 /superpowers 技能加載命令後發送，防止 AI 載入技能後直接自主掃描
			skillCmd := fmt.Sprintf("/superpowers:%s 請待命，等候我給予你具體的 Merge Request 評審任務。", skill)
			_ = exec.Command("tmux", "send-keys", "-t", sessionID, "-l", skillCmd).Run()
			time.Sleep(500 * time.Millisecond)
			_ = exec.Command("tmux", "send-keys", "-t", sessionID, "C-m").Run()

			// 由於技能加載與指令處理需要時間，我們主動等待其完成以避免干擾後續輸入
			time.Sleep(5 * time.Second)
			for i := 0; i < 45; i++ {
				checkCmd := exec.Command("tmux", "capture-pane", "-pt", sessionID)
				out, _ := checkCmd.Output()
				if w.isPromptReady(string(out)) {
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
			captureCmd := exec.Command("tmux", "capture-pane", "-pt", sessionID)
			lastScreen, _ := captureCmd.Output()
			fmt.Printf("[%s] SESSION DIED! Last screen output:\n%s\n", sessionID, string(lastScreen))

			fmt.Printf("[%s] Session exited. Cleaning up...\n", sessionID)
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

	fmt.Printf("[%s] Forwarding input: %s\n", sessionID, input)

	// 等待 AI 進入就緒狀態，避免在 AI 還在前一次處理中就發送新輸入
	for i := 0; i < 45; i++ {
		checkCmd := exec.Command("tmux", "capture-pane", "-pt", sessionID)
		out, _ := checkCmd.Output()
		if w.isPromptReady(string(out)) {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// 記錄發送問題前的終端歷史，以便後續比對出這次問題的回答內容
	linesBefore, err := w.getHistoryLines(sessionID)
	var N int
	if err == nil {
		N = len(linesBefore)
	}

	// 將輸入傳送到 tmux 視窗內，並模擬按下 Enter 鍵以執行
	_ = exec.Command("tmux", "send-keys", "-t", sessionID, "-l", input).Run()
	time.Sleep(500 * time.Millisecond)
	_ = exec.Command("tmux", "send-keys", "-t", sessionID, "C-m").Run()
	_ = exec.Command("tmux", "send-keys", "-t", sessionID, "C-m").Run()

	// 稍作延遲，確保指令已經開始在 tmux 中執行，避免馬上判定為 READY
	time.Sleep(2 * time.Second)

	// 持續偵測 tmux 畫面內容不再變化，且重新出現 Prompt 代表執行完畢
	maxPolls := 3600
	var lastScreen string
	sameCount := 0
	for poll := 0; poll < maxPolls; poll++ {
		time.Sleep(1 * time.Second)
		if w.isPaneDead(sessionID) {
			fmt.Printf("[%s] Detected pane dead during polling, extracting last output.\n", sessionID)
			break
		}
		checkCmd := exec.Command("tmux", "capture-pane", "-pt", sessionID)
		out, _ := checkCmd.Output()
		currScreen := string(out)

		currClean := cleanANSI(currScreen)
		lastClean := cleanANSI(lastScreen)

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

	// 抓取執行完畢後的完整歷史，並切分出新增的文字行
	linesAfter, err := w.getHistoryLines(sessionID)
	if err != nil {
		fmt.Printf("[%s] Error getting history after: %v\n", sessionID, err)
		return
	}

	var newLines []string
	if len(linesAfter) > N {
		newLines = linesAfter[N:]
	} else {
		newLines = linesAfter
	}

	// 過濾與清理每一行，移除 ANSI 控制字元與回顯問題等雜訊
	var cleanLines []string
	for _, line := range newLines {
		cleaned := cleanANSI(line)
		if w.shouldIgnore(cleaned) {
			continue
		}

		// 移除提示符並比對是否為輸入回顯，避免重複將問題傳回
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

	// 拼接成完整的回答
	fullText := strings.TrimSpace(strings.Join(cleanLines, "\n"))
	fullText = thoughtRegex.ReplaceAllString(fullText, "")
	fullText = strings.TrimSpace(fullText)

	if w.Config.OnlyFinalResponse || w.Config.ID == "reviewer" || w.Config.ID == "coder" {
		parts := dividerRegex.Split(fullText, -1)
		if len(parts) > 0 {
			finalText := strings.TrimSpace(parts[len(parts)-1])
			finalText = strings.TrimPrefix(finalText, "•")
			finalText = strings.TrimPrefix(finalText, "*")
			fullText = strings.TrimSpace(finalText)
		}
	}

	w.muLast.Lock()
	last := w.lastOutput
	w.muLast.Unlock()

	if fullText != "" && fullText != strings.TrimSpace(last) {
		// 確保記錄目錄存在，以便寫入答案檔案
		_ = os.MkdirAll(w.LogDir, 0755)
		answerFile := filepath.Join(w.LogDir, fmt.Sprintf("%s_answer.txt", sessionID))
		if err := os.WriteFile(answerFile, []byte(fullText), 0644); err != nil {
			fmt.Printf("[%s] Error writing answer file: %v\n", sessionID, err)
		} else {
			fmt.Printf("[%s] Wrote answer to file (%d bytes): %s\n", sessionID, len(fullText), fullText)
		}

		if w.outputCallback != nil {
			w.outputCallback(fullText)
		}
		w.muLast.Lock()
		w.lastOutput = fullText
		w.muLast.Unlock()
	}
}

// shouldIgnore 判定是否為無意義的系統噪音或 TUI 介面元素
func (w *Worker) shouldIgnore(text string) bool {
	t := strings.ToLower(text)
	// 濾掉 TUI 繪圖、狀態列關鍵字與動態加載符號
	noise := []string{
		"▀▀▀", "▄▄▄", "────", "───",
		"workspace (/", "branch", "sandbox", "auto (gemini",
		"type your message", "shift+tab", "? for shortcuts",
		"thinking...", "queued (press",
		"yolo mode is enabled",
		"using filekeychain fallback",
		"loaded cached credentials",
		"org.freedesktop.secrets",
		"working...", "⠏", "⠼", "⠴", "⠦", "⠧", // 加載動畫符號
		"press ctrl+o", "show more lines", // 終端狀態列提示
		"yolo ctrl+y", "mcp servers", "skills", // 狀態列關鍵字
		"quota", "used", "gemini 3", "gemini 1.5", // 狀態列剩餘額度等資訊
		"ctrl+c to stop", "ctrl+u to undo",
		"...", "✦",
	}
	for _, n := range noise {
		if strings.Contains(t, n) {
			return true
		}
	}
	// 濾掉過短或只有符號的訊息
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 2 {
		return true
	}
	return false
}

// 強化版 ANSI 清理正則
var ansiRegex = regexp.MustCompile(`[\x1B\x9B][[\]()#;?]*(?:(?:(?:[a-zA-Z\d]*(?:;[-a-zA-Z\d/#&.:=?%@~_]*)*)?\x07)|(?:(?:\d{1,4}(?:;\d{0,4})*)?[\dA-PR-TZcf-ntqry=><~]))`)

// 匹配不可見的控制字元與特定 Unicode 雜訊
var controlCharsRegex = regexp.MustCompile(`[\x00-\x1F\x7F-\x9F]`)

// 用於過濾 AI 回覆中的 CoT (Chain of Thought) 思考過程，僅回傳最終答案
var thoughtRegex = regexp.MustCompile(`(?s)<thought>.*?</thought>`)

// 用於識別並分割 CLI TUI 輸出中的步驟分割線，以提取最終的對話答案
var dividerRegex = regexp.MustCompile(`(?m)^[ \t]*[─\-\x{2500}]{5,}[ \t]*$`)

func cleanANSI(text string) string {
	// 1. 移除標準 ANSI 逃逸序列
	text = ansiRegex.ReplaceAllString(text, "")

	// 2. 處理 \r (Carriage Return): 模擬終端覆寫，只保留最後一部分
	if strings.Contains(text, "\r") {
		parts := strings.Split(text, "\r")
		for i := len(parts) - 1; i >= 0; i-- {
			p := strings.TrimSpace(parts[i])
			if p != "" {
				text = parts[i]
				break
			}
		}
	}

	// 3. 移除不可見的 ASCII 控制字元 (除了 \n)
	text = controlCharsRegex.ReplaceAllString(text, "")
	// 4. 移除特定的 Unicode 盲文符號 (常見於加載動畫)
	text = strings.Map(func(r rune) rune {
		if r >= '\u2800' && r <= '\u28FF' { // Braille Patterns
			return -1
		}
		return r
	}, text)
	return strings.TrimSpace(text)
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
