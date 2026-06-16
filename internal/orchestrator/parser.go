package orchestrator

import (
	"regexp"
	"strings"
)

var (
	// 強化版 ANSI 清理正則
	ansiRegex = regexp.MustCompile(`[\x1B\x9B][[\]()#;?]*(?:(?:(?:[a-zA-Z\d]*(?:;[-a-zA-Z\d/#&.:=?%@~_]*)*)?\x07)|(?:(?:\d{1,4}(?:;\d{0,4})*)?[\dA-PR-TZcf-ntqry=><~]))`)

	// 匹配不可見的控制字元與特定 Unicode 雜訊
	controlCharsRegex = regexp.MustCompile(`[\x00-\x1F\x7F-\x9F]`)

	// 用於過濾 AI 回覆中的 CoT (Chain of Thought) 思考過程，僅回傳最終答案
	thoughtRegex = regexp.MustCompile(`(?s)<thought>.*?</thought>`)

	// 用於識別並分割 CLI TUI 輸出中的步驟分割線，以提取最終的對話答案
	dividerRegex = regexp.MustCompile(`(?m)^[ \t]*[─\-\x{2500}]{5,}[ \t]*$`)
)

// CleanLine 處理單行文字的清理，確保輸出不含 ANSI 逃逸序列、不可見字元及動態加載雜訊
func CleanLine(text string) string {
	text = ansiRegex.ReplaceAllString(text, "")

	// 模擬終端覆寫行為，當存在 Carriage Return 時僅保留最後一段有效內容
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

	text = controlCharsRegex.ReplaceAllString(text, "")

	// 移除盲文符號，這些符號通常用於 CLI 的動態加載動畫，在純文字日誌中無意義
	text = strings.Map(func(r rune) rune {
		if r >= '\u2800' && r <= '\u28FF' {
			return -1
		}
		return r
	}, text)

	return strings.TrimSpace(text)
}

// CleanBlock 處理完整回答區塊的清理 (CoT 思考過程)
func CleanBlock(text string) string {
	// 移除 AI 的思考過程 (可能跨行)
	text = thoughtRegex.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// ParseFinalResponse 從完整文字中提取最終回答 (處理分割線)
func ParseFinalResponse(text string) string {
	parts := dividerRegex.Split(text, -1)
	if len(parts) > 0 {
		finalText := strings.TrimSpace(parts[len(parts)-1])
		finalText = strings.TrimPrefix(finalText, "•")
		finalText = strings.TrimPrefix(finalText, "*")
		return strings.TrimSpace(finalText)
	}
	return text
}

func ShouldIgnore(text string) bool {
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
