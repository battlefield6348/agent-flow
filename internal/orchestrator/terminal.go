package orchestrator

import (
	"context"
)

// Terminal 定義了與終端 (如 tmux) 互動的介面
type Terminal interface {
	// Start 啟動一個新的終端對話
	Start(ctx context.Context, sessionID string, workspace string, cmd string, env []string) error
	// Stop 停止指定的對話
	Stop(sessionID string) error
	// SendKeys 向指定的對話發送按鍵指令
	SendKeys(sessionID string, keys string, enter bool) error
	// CapturePane 抓取當前畫面的內容
	CapturePane(sessionID string) (string, error)
	// CaptureHistory 抓取對話的完整歷史
	CaptureHistory(sessionID string) ([]string, error)
	// HasSession 檢查對話是否仍存在
	HasSession(sessionID string) bool
	// IsPaneDead 檢查 Pane 是否已經結束執行
	IsPaneDead(sessionID string) bool
}
