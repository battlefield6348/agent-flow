package telegram

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LogPoller struct {
	bot            *Bot
	logDir         string
	allowedChatIDs []int64
	stopCh         chan struct{}
}

func NewLogPoller(bot *Bot, logDir string, allowedChatIDs []int64) *LogPoller {
	return &LogPoller{
		bot:            bot,
		logDir:         logDir,
		allowedChatIDs: allowedChatIDs,
		stopCh:         make(chan struct{}),
	}
}

func (p *LogPoller) Start() {
	// 為每個 Agent 啟動一個 goroutine 追蹤日誌
	for _, cfg := range p.bot.configs {
		go p.tailFile(cfg.ID)
	}
}

func (p *LogPoller) tailFile(agentID string) {
	logPath := filepath.Join(p.logDir, fmt.Sprintf("%s.log", agentID))
	
	// 等待檔案建立
	for {
		if _, err := os.Stat(logPath); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}

	file, err := os.Open(logPath)
	if err != nil {
		fmt.Printf("[Poller] Error opening %s: %v\n", logPath, err)
		return
	}
	defer file.Close()

	// 移至檔案末尾，只讀取新內容
	_, _ = file.Seek(0, io.SeekEnd)
	reader := bufio.NewReader(file)

	// Coalescing 機制：收集一段時間內的訊息再一起發送
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
		text := fmt.Sprintf("*[%s]*\n%s", agentID, strings.Join(buffer, ""))
		for _, chatID := range p.allowedChatIDs {
			p.bot.SendMessage(chatID, text)
		}
		buffer = nil
		timer = nil
	}

	for {
		line, err := reader.ReadString('\n')
		if err == nil {
			mu.Lock()
			buffer = append(buffer, line)
			if timer == nil {
				timer = time.AfterFunc(2*time.Second, sendBuffer)
			}
			mu.Unlock()
		} else if err == io.EOF {
			time.Sleep(500 * time.Millisecond)
		} else {
			return
		}

		select {
		case <-p.stopCh:
			return
		default:
			continue
		}
	}
}
