package orchestrator

import (
	"bufio"
	"fmt"
	"io"
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
}

func (w *Worker) SetOutputCallback(cb func(string)) {
	w.outputCallback = cb
}

func (w *Worker) IsRunning() bool {
	checkCmd := exec.Command("tmux", "has-session", "-t", w.Config.ID)
	err := checkCmd.Run()
	if err != nil {
		// 捕捉最後的畫面以便除錯
		captureCmd := exec.Command("tmux", "capture-pane", "-pt", w.Config.ID)
		lastScreen, _ := captureCmd.Output()
		if len(lastScreen) > 0 {
			fmt.Printf("[%s] Last screen before ending:\n%s\n", w.Config.ID, string(lastScreen))
		}
	}
	return err == nil
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
	w.inputCh <- text
}

func (w *Worker) Start() {
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

// isPromptReady 檢查畫面是否出現可輸入的提示
func (w *Worker) isPromptReady(screen string) bool {
	// 濾掉 Thinking 狀態
	lower := strings.ToLower(screen)
	if strings.Contains(lower, "thinking") || strings.Contains(lower, "queued") || strings.Contains(lower, "working") {
		return false
	}

	// 支援多種常見的提示字元 (包含 Codex 的特殊符號)
	prompts := []string{
		">",
		"›", // Codex 特有的 Unicode 提示符
		"»",
		"Type your message",
		"workspace (",
		"shift+tab",
		"gpt-5.3-codex", // Codex 狀態列
	}

	for _, p := range prompts {
		if strings.Contains(screen, p) {
			return true
		}
	}
	return false
}

func (w *Worker) runProcess() {
	sessionID := w.Config.ID
	// 啟動前確保乾淨，但不要在循環中頻繁呼叫
	_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()

	fmt.Printf("[%s] Engine started in tmux (PTY) mode.\n", sessionID)

	argsStr := strings.TrimSpace(strings.Join(w.Config.Args, " "))
	var fullCmd string
	if argsStr != "" {
		fullCmd = fmt.Sprintf("%s %s", w.Config.Cmd, argsStr)
	} else {
		fullCmd = w.Config.Cmd
	}

	startCmd := exec.Command("tmux", "new-session", "-d", "-s", sessionID, "sh", "-c", fullCmd)
	startCmd.Env = os.Environ()

	if err := startCmd.Run(); err != nil {
		fmt.Printf("[%s] CRITICAL: Failed to run tmux start: %v\n", sessionID, err)
		return
	}

	// 等待初始化
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

	// 啟動輸入監聽
	stopInput := make(chan struct{})
	go func() {
		for {
			select {
			case input := <-w.inputCh:
				fmt.Printf("[%s] Forwarding input: %s\n", sessionID, input)

				// 嘗試等待 Prompt 出現，最多試 3 次
				for i := 0; i < 3; i++ {
					checkCmd := exec.Command("tmux", "capture-pane", "-pt", sessionID)
					out, _ := checkCmd.Output()
					if w.isPromptReady(string(out)) {
						break
					}
					time.Sleep(2 * time.Second)
				}

				// 直接打字並發送
				_ = exec.Command("tmux", "send-keys", "-t", sessionID, "-l", input).Run()
				time.Sleep(500 * time.Millisecond)
				_ = exec.Command("tmux", "send-keys", "-t", sessionID, "C-m").Run()
				_ = exec.Command("tmux", "send-keys", "-t", sessionID, "C-m").Run()
			case <-stopInput:
				return
			case <-w.stopCh:
				return
			}
		}
	}()

	// 啟動日誌監控
	_ = os.MkdirAll(w.LogDir, 0755)
	logPath := filepath.Join(w.LogDir, fmt.Sprintf("%s.log", sessionID))
	_ = exec.Command("tmux", "pipe-pane", "-t", sessionID, fmt.Sprintf("cat >> %s", logPath)).Run()
	go w.tailLogFile(logPath)

	// 初始指令
	if w.Config.InitialInstruction != "" {
		time.Sleep(3 * time.Second)
		w.SendInput(w.Config.InitialInstruction)
	}

	// 存活檢查循環
	for {
		time.Sleep(10 * time.Second)
		if !w.IsRunning() {
			// 在正式關閉前，強行抓取最後的畫面日誌
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

func (w *Worker) tailLogFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	_, _ = file.Seek(0, io.SeekEnd)
	reader := bufio.NewReader(file)

	var (
		buffer []string
		mu     sync.Mutex
		timer  *time.Timer
	)

	sendBuffer := func() {
		mu.Lock()
		defer mu.Unlock()
		if len(buffer) == 0 {
			return
		}

		// 關鍵優化：逐行處理，只保留非雜訊行
		var cleanLines []string
		for _, line := range buffer {
			if !w.shouldIgnore(line) {
				cleanLines = append(cleanLines, line)
			}
		}

		buffer = nil
		timer = nil

		if len(cleanLines) == 0 {
			return
		}

		fullText := strings.TrimSpace(strings.Join(cleanLines, ""))

		w.muLast.Lock()
		last := w.lastOutput
		w.muLast.Unlock()

		// 檢查是否與上次發送的內容完全相同 (忽略空白)
		if fullText == "" || fullText == strings.TrimSpace(last) {
			return
		}

		// 如果新內容只是舊內容的開頭（代表是重複或不完整的更新），則忽略
		if strings.HasPrefix(strings.TrimSpace(last), fullText) && len(fullText) < len(strings.TrimSpace(last)) {
			return
		}

		if w.outputCallback != nil {
			w.outputCallback(fullText)
		}

		w.muLast.Lock()
		w.lastOutput = fullText
		w.muLast.Unlock()
	}

	for {
		line, err := reader.ReadString('\n')
		if err == nil {
			clean := cleanANSI(line)
			trimmed := strings.TrimSpace(clean)

			w.muLast.Lock()
			last := w.lastOutput
			w.muLast.Unlock()

			// 過濾掉重複的單行更新與空白
			if trimmed != "" && trimmed != strings.TrimSpace(last) {
				mu.Lock()
				buffer = append(buffer, clean)
				if timer == nil {
					timer = time.AfterFunc(3*time.Second, sendBuffer)
				}
				mu.Unlock()
			}
		} else {
			time.Sleep(500 * time.Millisecond)
		}

		select {
		case <-w.stopCh:
			return
		default:
		}
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
