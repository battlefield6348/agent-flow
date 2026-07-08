package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Worker struct {
	Config              CollaboratorConfig
	LogDir              string
	Terminal            Terminal
	probeGitLabUsername func(workspace string, env []string) (string, error)
	stopCh              chan struct{}
	inputCh             chan WorkerTask
	outputCallback      func(string)
	lastOutput          string
	muLast              sync.Mutex
	isBusy              bool
	muBusy              sync.Mutex
	running             bool
	muRun               sync.Mutex
}

type WorkerTask struct {
	Text                   string
	ExpectedGitLabUsername string
	OnSuccess              func(string)
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
		Config:              cfg,
		LogDir:              logDir,
		Terminal:            terminal,
		probeGitLabUsername: probeGitLabUsername,
		stopCh:              make(chan struct{}),
		inputCh:             make(chan WorkerTask, 10),
	}
}

func (w *Worker) BuildPromptMsg(sessionID string) string {
	var promptMsg string
	switch sessionID {
	case agentIDCoder:
		promptMsg = "請待命，等候我給予你具體的開發與修正任務。"
	case agentIDReviewer:
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
	w.SendTask(WorkerTask{Text: text})
}

func (w *Worker) SendTask(task WorkerTask) {
	text := task.Text
	if w.Config.InputPrefix != "" {
		text = w.Config.InputPrefix + text
	}
	task.Text = text
	w.inputCh <- task
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

// isPromptReady 檢查畫面最後幾行是否出現可輸入的提示，避免歷史中的舊提示符或標題列造成誤判
// spinnerLineRegex 比對 Claude Code v2 的進行中 spinner 行，如「✢ Seasoning…」
// 「✻ Fermenting… (5m 47s · …)」。動詞為隨機字彙，只能靠「行首符號＋單字＋…」形態辨識；
// 完成後的「✻ Brewed for 40s」沒有「…」，不會誤判。
var spinnerLineRegex = regexp.MustCompile(`^\s*[·✢✳✻✽∗＊+*]\s*[A-Za-z]+(\s[A-Za-z]+)?…`)

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

	// 忙碌訊號必須掃「整個畫面」而非只看最後幾行：Claude Code v2 的頁尾
	// （「bypass permissions on (shift+tab to cycle)」）永遠固定在最底部，
	// 而 spinner（「✻ Fermenting… (5m 47s · thinking …)」）與排隊提示
	// （「Press up to edit queued messages」）都顯示在畫面較上方——只掃底部
	// 會誤判為就緒，導致任務尚未執行完就提早觸發完成回呼。
	// 另外 spinner 剛起步的幾秒只有「✢ Seasoning…」這種動詞行（動詞隨機、無
	// elapsed/tokens 資訊），關鍵字比對不到，必須用「符號＋動詞＋…」的形態偵測。
	for _, row := range rawLines[:end] {
		lower := strings.ToLower(row)
		if strings.Contains(lower, "thinking") || strings.Contains(lower, "queued") || strings.Contains(lower, "working") ||
			strings.Contains(lower, "esc to interrupt") || strings.Contains(lower, "tokens ·") {
			return false
		}
		if spinnerLineRegex.MatchString(row) {
			return false
		}
	}

	// 檢查最後幾行是否為「確認對話框 / 選單」，若是則代表 CLI 正等待「選擇」而非等待「輸入」，
	// 必須視為未就緒——否則選單中的 ❯（如 Claude 的 --dangerously-skip-permissions 首次接受畫面
	// 「❯ 1. No, exit」）會讓下方的提示字元偵測誤判成就緒，導致注入的指令被送進對話框而遺失。
	// 注意：這些字串刻意避開就緒狀態頁尾的字樣（如「bypass permissions on」「esc to interrupt」）。
	dialogMarkers := []string{
		"do you want to proceed",
		"esc to cancel",
		"no, exit",
		"yes, i accept",
		"bypass permissions mode",
		"press enter to continue",
	}
	for _, row := range lastRows {
		lower := strings.ToLower(row)
		for _, m := range dialogMarkers {
			if strings.Contains(lower, m) {
				return false
			}
		}
		// 選單箭頭指向編號選項（如「❯ 1. ...」）也代表在等待選擇而非輸入
		trimmed := strings.TrimSpace(row)
		if rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "❯")); rest != trimmed {
			if len(rest) >= 2 && rest[0] >= '1' && rest[0] <= '9' && rest[1] == '.' {
				return false
			}
		}
	}

	// 檢查最後幾行是否包含提示字元
	prompts := []string{
		">",
		"›",
		"»",
		"❯", // Claude Code v2 的輸入提示符 (U+276F)
		"%",
		"➜",
		"Type your message",
		"workspace (",
		"shift+tab",
		"gpt-5.3-codex",
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

func (w *Worker) runProcess() {
	sessionID := w.Config.ID
	slog.Info("Worker engine starting", "worker_id", sessionID)

	var additionalArgs []string
	var superpowersSkills []string
	// 各 CLI 讀取 skill 的基底目錄不同：agy(Antigravity)原生 /技能名 只認它自己的
	// ~/.gemini/antigravity/skills；claude/codex 靠 --add-dir + 明確路徑，用中性的
	// ~/.agent-flow/skills。兩個目錄都由 `make install-skills` 一次安裝，故 skill
	// 只需維護在 repo 的 skills/，三種 CLI 都吃得到。
	skillsBase := ".agent-flow/skills"
	if strings.Contains(w.Config.Cmd, "agy") {
		skillsBase = ".gemini/antigravity/skills"
	}
	homeDir, err := os.UserHomeDir()
	if err == nil {
		for _, skill := range w.Config.Skills {
			// 移除可能存在的 "superpowers:" 前綴以取得正確的本地技能目錄名稱
			skillName := skill
			if strings.HasPrefix(skill, "superpowers:") {
				skillName = strings.TrimPrefix(skill, "superpowers:")
			}
			skillPath := filepath.Join(homeDir, skillsBase, skillName)
			if _, err := os.Stat(skillPath); err == nil {
				if !strings.Contains(w.Config.Cmd, "agy") {
					additionalArgs = append(additionalArgs, "--add-dir", skillPath)
				}
				superpowersSkills = append(superpowersSkills, skillName)
			}
		}
	}

	baseArgs := normalizeCLIArgs(w.Config.Cmd, w.Config.Args)
	allArgs := append(baseArgs, additionalArgs...)
	argsStr := strings.TrimSpace(strings.Join(allArgs, " "))
	var fullCmd string
	if argsStr != "" {
		fullCmd = fmt.Sprintf("%s %s", w.Config.Cmd, argsStr)
	} else {
		fullCmd = w.Config.Cmd
	}
	slog.Info("Starting worker command", "worker_id", sessionID, "cmd", fullCmd)

	cleanEnv := w.buildProcessEnv()

	if err := w.Terminal.Start(context.Background(), sessionID, w.Config.Workspace, fullCmd, cleanEnv); err != nil {
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

	if ready && len(superpowersSkills) > 0 {
		time.Sleep(5 * time.Second)
		for _, skill := range superpowersSkills {
			slog.Info("Injecting skill", "worker_id", sessionID, "skill", skill)
			promptMsg := w.BuildPromptMsg(sessionID)
			var skillCmd string
			if strings.Contains(w.Config.Cmd, "agy") {
				// Antigravity 原生技能，透過 /技能名 呼叫
				skillCmd = fmt.Sprintf("/%s %s", skill, promptMsg)
			} else {
				// claude / codex 等 CLI 無此原生指令，技能檔已透過 --add-dir 掛入，
				// 改以自然語言指示其讀取並遵循該技能檔的工作流程
				skillPath := filepath.Join(homeDir, skillsBase, skill)
				skillCmd = fmt.Sprintf("請先閱讀並嚴格遵循技能檔 %s/SKILL.md 內的工作流程，然後%s", skillPath, promptMsg)
			}
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
			case task := <-w.inputCh:
				w.handleInput(task, sessionID)
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

func (w *Worker) buildProcessEnv() []string {
	var cleanEnv []string
	hasTerm := false
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "VSCODE_") || strings.HasPrefix(env, "ANTIGRAVITY_") || strings.HasPrefix(env, "TERM_PROGRAM=") || strings.HasPrefix(env, "ELECTRON_RUN_AS_NODE=") || strings.HasPrefix(env, "GITLAB_TOKEN=") {
			continue
		}
		if strings.HasPrefix(env, "TERM=") {
			cleanEnv = append(cleanEnv, "TERM=screen-256color")
			hasTerm = true
			continue
		}
		if strings.HasPrefix(env, "PATH=") {
			pathVal := strings.TrimPrefix(env, "PATH=")
			var cleanPaths []string
			for _, p := range strings.Split(pathVal, ":") {
				if !strings.Contains(p, "remote-cli") {
					cleanPaths = append(cleanPaths, p)
				}
			}
			cleanEnv = append(cleanEnv, "PATH="+strings.Join(cleanPaths, ":"))
			continue
		}
		cleanEnv = append(cleanEnv, env)
	}
	if !hasTerm {
		cleanEnv = append(cleanEnv, "TERM=screen-256color")
	}

	// 注入此 collaborator 專屬的 GitLab token,讓 CLI 在 workspace 內以此身分留言/發 note。
	// 沒有這一步時,CLI 會退回讀 glab 的登入設定檔(通常是使用者本人),導致審查留言掛錯帳號
	// (例如 reviewer 角色應顯示為 bot 卻顯示成使用者)。glab 會優先採用 GITLAB_TOKEN 環境變數,
	// 目標 host 由 workspace 的 git remote 自動解析,故不需另設 GITLAB_HOST。
	if w.Config.GitLabToken != "" {
		cleanEnv = append(cleanEnv, "GITLAB_TOKEN="+w.Config.GitLabToken)
	}
	return cleanEnv
}

func probeGitLabUsername(workspace string, env []string) (string, error) {
	// glab api 沒有 --jq 這種欄位篩選旗標(不同於 GitHub 的 gh api),
	// 因此這裡取得完整 JSON 後自行解析 username 欄位。
	cmd := exec.Command("glab", "api", "/user")
	cmd.Dir = workspace
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("glab api /user failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(out, &user); err != nil {
		return "", fmt.Errorf("glab api /user returned invalid JSON: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return user.Username, nil
}

func normalizeCLIArgs(cmd string, args []string) []string {
	if !strings.Contains(strings.ToLower(cmd), "codex") {
		return append([]string(nil), args...)
	}

	normalized := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "--dangerously-bypass-hook") && arg != "--dangerously-bypass-hook-trust" {
			slog.Warn("Normalizing suspicious codex hook trust flag", "original", arg, "normalized", "--dangerously-bypass-hook-trust")
			normalized = append(normalized, "--dangerously-bypass-hook-trust")
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

// handleInput 處理來自通道的指令，並追蹤其忙碌狀態與結果輸出
func (w *Worker) handleInput(task WorkerTask, sessionID string) {
	w.setBusy(true)
	defer w.setBusy(false)

	input := task.Text
	slog.Info("Forwarding input to worker", "worker_id", sessionID, "input", input)

	if !w.waitForReady(45) {
		slog.Warn("Worker not ready for input, proceeding anyway", "worker_id", sessionID)
	}

	if task.ExpectedGitLabUsername != "" {
		actual, err := w.probeGitLabUsername(w.Config.Workspace, w.buildProcessEnv())
		if err != nil {
			slog.Error("Failed to verify glab identity before task", "worker_id", sessionID, "expected_username", task.ExpectedGitLabUsername, "error", err)
			return
		}
		if actual != task.ExpectedGitLabUsername {
			slog.Error("Refusing to run task with unexpected glab identity", "worker_id", sessionID, "expected_username", task.ExpectedGitLabUsername, "actual_username", actual)
			return
		}
	}

	// 記錄發送指令前的歷史行數，以便後續提取增量回覆
	nBefore := w.getHistoryLineCount(sessionID)

	_ = w.Terminal.SendKeys(sessionID, input, true)
	_ = w.Terminal.SendKeys(sessionID, "", true) // 額外的 Enter 確保 CLI 觸發執行

	// 等待執行完成並提取輸出
	if !w.pollUntilReady(3600) {
		slog.Warn("Polling timeout or interrupted", "worker_id", sessionID)
	}

	if output, ok := w.processAndSaveOutput(sessionID, nBefore, input); ok && task.OnSuccess != nil {
		task.OnSuccess(output)
	}
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
func (w *Worker) pollUntilReady(maxSeconds int) bool {
	var lastScreen string
	sameCount := 0
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
			sameCount = 0
		}
		lastScreen = currScreen

		// 當畫面連續兩秒無變化且出現提示符時，視為執行結束
		if sameCount >= 2 && w.isPromptReady(currScreen) {
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
func (w *Worker) processAndSaveOutput(sessionID string, nBefore int, originalInput string) (string, bool) {
	linesAfter, err := w.Terminal.CaptureHistory(sessionID)
	if err != nil {
		slog.Error("Error getting terminal history", "worker_id", sessionID, "error", err)
		return "", false
	}

	var newLines []string
	if len(linesAfter) > nBefore {
		newLines = linesAfter[nBefore:]
	} else {
		newLines = linesAfter
	}

	return w.processAndSaveOutputFromLines(sessionID, newLines, originalInput)
}

func (w *Worker) processAndSaveOutputFromLines(sessionID string, lines []string, originalInput string) (string, bool) {
	fullText := w.filterAndJoinLines(lines, originalInput)
	if fullText == "" {
		return "", false
	}

	if HasFatalOutput(fullText) {
		slog.Error("Worker produced fatal terminal output", "worker_id", sessionID, "output", fullText)
		return "", false
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
	return fullText, true
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

	if w.Config.OnlyFinalResponse || w.Config.ID == agentIDReviewer || w.Config.ID == agentIDCoder {
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
