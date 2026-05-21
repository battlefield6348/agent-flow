package telegram

import (
	"fmt"
	"html"
	"log"
	"strings"

	"gemini-collaborator-go/internal/orchestrator"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api            *tgbotapi.BotAPI
	allowedChatIDs []int64
	configs        []orchestrator.CollaboratorConfig
	manager        *orchestrator.WorkerManager
	activeAgent    map[int64]string // User ID -> Agent ID
	defaultChatID  int64            // 指揮部的主要群組 ID
}

func NewBot(token string, allowedChatIDs []int64, configs []orchestrator.CollaboratorConfig, manager *orchestrator.WorkerManager) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	b := &Bot{
		api:            api,
		allowedChatIDs: allowedChatIDs,
		configs:        configs,
		manager:        manager,
		activeAgent:    make(map[int64]string),
	}

	// 如果有設定 AllowedChatIDs，預設第一個為指揮部群組
	if len(allowedChatIDs) > 0 {
		b.defaultChatID = allowedChatIDs[0]
	}

	return b, nil
}

func (b *Bot) Start() {
	log.Printf("[TG] War Room initialized on account %s", b.api.Self.UserName)

	// 1. 如果已經有預設的 ChatID，主動發送上線通知
	if b.defaultChatID != 0 {
		b.SendHTML(b.defaultChatID, "🚀 <b>AI 開發指揮部已上線</b>\n您可以開始使用 <code>#planner</code> 或 <code>#coder</code> 下達指令了。")
	} else {
		fmt.Println("----------------------------------------------------")
		fmt.Println("NOTICE: No ChatID configured.")
		fmt.Println("Please send ANY message to the Bot in your Telegram group to initialize the link.")
		fmt.Println("----------------------------------------------------")
	}

	// 2. 設定 Worker 的輸出回調...
	for _, w := range b.manager.Workers {
		workerName := w.Config.Name
		w.SetOutputCallback(func(text string) {
			if b.defaultChatID != 0 {
				formatted := fmt.Sprintf("🤖 <b>[%s]</b>\n%s", html.EscapeString(workerName), html.EscapeString(text))
				b.SendHTML(b.defaultChatID, formatted)
			}
		})
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID
		// 自動學習第一個傳入訊息的群組 ID 作為指揮部 (如果尚未設定)
		if b.defaultChatID == 0 {
			b.defaultChatID = chatID
			log.Printf("[TG] Auto-set default ChatID to %d", chatID)
		}

		if !b.isAllowed(chatID) {
			continue
		}

		b.handleMessage(update.Message)
	}
}

func (b *Bot) isAllowed(chatID int64) bool {
	if len(b.allowedChatIDs) == 0 {
		return true
	}
	for _, id := range b.allowedChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

func (b *Bot) handleMessage(m *tgbotapi.Message) {
	text := m.Text

	if strings.HasPrefix(text, "/") {
		b.handleCommand(m)
		return
	}

	// 標籤路由
	var routed bool
	for _, cfg := range b.configs {
		if cfg.Tag == "" {
			continue
		}
		targetTag := "#" + strings.ToLower(cfg.Tag)
		if strings.Contains(strings.ToLower(text), targetTag) {
			// 關鍵優化：在發送給 AI 前，先過濾掉標籤本身，避免 AI 回覆時又帶標籤
			cleanedText := strings.ReplaceAll(text, targetTag, "")
			cleanedText = strings.TrimSpace(cleanedText)

			for _, w := range b.manager.Workers {
				if w.Config.ID == cfg.ID {
					w.SendInput(cleanedText)
					routed = true
				}
			}
		}
	}

	if routed {
		return
	}

	// 回退到 Active Agent
	agentID, ok := b.activeAgent[m.Chat.ID]
	if ok {
		for _, w := range b.manager.Workers {
			if w.Config.ID == agentID {
				w.SendInput(text)
				return
			}
		}
	}

	msg := tgbotapi.NewMessage(m.Chat.ID, "No agent specified. Use #tag or /select.")
	b.api.Send(msg)
}

func (b *Bot) handleCommand(m *tgbotapi.Message) {
	parts := strings.Fields(m.Text)
	cmd := parts[0]

	switch cmd {
	case "/list":
		var res []string
		for _, c := range b.configs {
			status := "🔴 Offline"
			for _, w := range b.manager.Workers {
				if w.Config.ID == c.ID && w.IsRunning() {
					status = "🟢 Online"
					break
				}
			}
			res = append(res, fmt.Sprintf("- <code>%s</code>: %s (%s) [Tag: #%s]",
				html.EscapeString(c.ID),
				html.EscapeString(c.Name),
				html.EscapeString(status),
				html.EscapeString(c.Tag)))
		}
		b.SendHTML(m.Chat.ID, "<b>Available Agents:</b>\n"+strings.Join(res, "\n"))

	case "/select":
		if len(parts) < 2 {
			b.SendHTML(m.Chat.ID, "Usage: <code>/select [agent_id]</code>")
			return
		}
		agentID := parts[1]
		b.activeAgent[m.Chat.ID] = agentID
		b.SendHTML(m.Chat.ID, fmt.Sprintf("✅ Selected: <code>%s</code>", html.EscapeString(agentID)))

	case "/status":
		active, _ := b.activeAgent[m.Chat.ID]
		b.SendHTML(m.Chat.ID, fmt.Sprintf("Active Session: <code>%s</code>", html.EscapeString(active)))

	case "/help", "/start":
		help := "<b>Commands:</b>\n/list - Agents status\n/select [id] - Select agent\n/status - Current status"
		b.SendHTML(m.Chat.ID, help)
	}
}

func (b *Bot) SendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.api.Send(msg)
}

func (b *Bot) SendHTML(chatID int64, htmlText string) {
	msg := tgbotapi.NewMessage(chatID, htmlText)
	msg.ParseMode = "HTML"
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("[TG] Failed to send HTML message: %v", err)
	}
}

// HandleInitialTask 用於在啟動時模擬接收到一個指令
func (b *Bot) HandleInitialTask(text string) {
	if b.defaultChatID == 0 {
		log.Println("[TG] Cannot send initial task: No default ChatID set.")
		return
	}

	// 模擬一個訊息物件
	fakeMsg := &tgbotapi.Message{
		Text: text,
		Chat: &tgbotapi.Chat{ID: b.defaultChatID},
	}

	// 先發送這條訊息到群組，讓使用者看到任務已啟動
	b.SendHTML(b.defaultChatID, fmt.Sprintf("📢 <b>系統啟動自動任務</b>:\n%s", html.EscapeString(text)))

	// 進入正常的處理流程邏輯
	b.handleMessage(fakeMsg)
}
