package telegram

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gemini-collaborator-go/internal/orchestrator"
)

type Bot struct {
	api            *tgbotapi.BotAPI
	allowedChatIDs []int64
	configs        []orchestrator.CollaboratorConfig
	activeAgent    map[int64]string // User ID -> Agent ID
}

func NewBot(token string, allowedChatIDs []int64, configs []orchestrator.CollaboratorConfig) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:            api,
		allowedChatIDs: allowedChatIDs,
		configs:        configs,
		activeAgent:    make(map[int64]string),
	}, nil
}

func (b *Bot) Start() {
	log.Printf("[TG] Authorized on account %s", b.api.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		// 檢查授權
		if !b.isAllowed(update.Message.Chat.ID) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Unauthorized access.")
			b.api.Send(msg)
			continue
		}

		b.handleMessage(update.Message)
	}
}

func (b *Bot) isAllowed(chatID int64) bool {
	if len(b.allowedChatIDs) == 0 {
		return true // 如果沒設定則預設允許 (開發用)
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

	// 1. 處理指令
	if strings.HasPrefix(text, "/") {
		b.handleCommand(m)
		return
	}

	// 2. 處理對話 (轉發給 Active Agent)
	agentID, ok := b.activeAgent[m.Chat.ID]
	if !ok {
		msg := tgbotapi.NewMessage(m.Chat.ID, "Please select an agent first using /select [id]")
		b.api.Send(msg)
		return
	}

	// 透過 tmux send-keys 轉發
	fmt.Printf("[TG] Forwarding to %s: %s\n", agentID, text)
	cmd := exec.Command("tmux", "send-keys", "-t", agentID, text, "Enter")
	if err := cmd.Run(); err != nil {
		msg := tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("Error sending to tmux: %v", err))
		b.api.Send(msg)
	}
}

func (b *Bot) handleCommand(m *tgbotapi.Message) {
	parts := strings.Fields(m.Text)
	cmd := parts[0]

	switch cmd {
	case "/list":
		var res []string
		for _, c := range b.configs {
			res = append(res, fmt.Sprintf("- `%s`: %s", c.ID, c.Name))
		}
		msg := tgbotapi.NewMessage(m.Chat.ID, "Available Agents:\n"+strings.Join(res, "\n"))
		msg.ParseMode = "Markdown"
		b.api.Send(msg)

	case "/select":
		if len(parts) < 2 {
			msg := tgbotapi.NewMessage(m.Chat.ID, "Usage: /select [agent_id]")
			b.api.Send(msg)
			return
		}
		agentID := parts[1]
		// 檢查 agent 是否存在於設定中
		found := false
		for _, c := range b.configs {
			if c.ID == agentID {
				found = true
				break
			}
		}
		if found {
			b.activeAgent[m.Chat.ID] = agentID
			msg := tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("Selected Agent: `%s`. You can now send messages directly.", agentID))
			msg.ParseMode = "Markdown"
			b.api.Send(msg)
		} else {
			msg := tgbotapi.NewMessage(m.Chat.ID, "Agent not found.")
			b.api.Send(msg)
		}

	case "/help":
		help := "/list - List agents\n/select [id] - Select agent for chat\n/status - Check session status"
		msg := tgbotapi.NewMessage(m.Chat.ID, help)
		b.api.Send(msg)
	}
}

func (b *Bot) SendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.api.Send(msg)
}
