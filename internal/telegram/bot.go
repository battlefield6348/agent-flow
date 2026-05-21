package telegram

import (
	"fmt"
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
		b.SendMessage(b.defaultChatID, "🚀 *AI 開發指揮部已上線*\n您可以開始使用 `#planner` 或 `#coder` 下達指令了。")
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
				formatted := fmt.Sprintf("🤖 *[%s]*\n%s", workerName, text)
				msg := tgbotapi.NewMessage(b.defaultChatID, formatted)
				msg.ParseMode = "Markdown"
				_, err := b.api.Send(msg)
				if err != nil {
					log.Printf("[TG] Failed to send worker output: %v", err)
				}
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
			res = append(res, fmt.Sprintf("- `%s`: %s (%s) [Tag: #%s]", c.ID, c.Name, status, c.Tag))
		}
		msg := tgbotapi.NewMessage(m.Chat.ID, "*Available Agents:*\n"+strings.Join(res, "\n"))
		msg.ParseMode = "Markdown"
		b.api.Send(msg)

	case "/select":
		if len(parts) < 2 {
			msg := tgbotapi.NewMessage(m.Chat.ID, "Usage: `/select [agent_id]`")
			b.api.Send(msg)
			return
		}
		agentID := parts[1]
		b.activeAgent[m.Chat.ID] = agentID
		msg := tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("✅ Selected: `%s`", agentID))
		b.api.Send(msg)

	case "/status":
		active, _ := b.activeAgent[m.Chat.ID]
		msg := tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("Active Session: `%s`", active))
		msg.ParseMode = "Markdown"
		b.api.Send(msg)

	case "/help", "/start":
		help := "/list - Agents status\n/select [id] - Select agent\n/status - Current status"
		msg := tgbotapi.NewMessage(m.Chat.ID, help)
		b.api.Send(msg)
	}
}

func (b *Bot) SendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.api.Send(msg)
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
	b.SendMessage(b.defaultChatID, fmt.Sprintf("📢 *系統啟動自動任務*:\n%s", text))

	// 進入正常的處理流程邏輯
	b.handleMessage(fakeMsg)
}
