.PHONY: build start stop setup cao-status clean fmt test check-tools docker-build docker-up docker-down docker-logs

# 基本變數設定
BINARY_NAME=agent-flow
MAIN_PATH=./cmd/agent-flow/main.go

# 一鍵初始化 CAO 必備環境與 Sessions
setup:
	@bash ./scripts/bootstrap-cao.sh

# 一鍵啟動 (自動初始化 CAO 並啟動 Agent Flow 背景輪詢)
start: setup
	@echo "🚀 正在啟動 Agent Flow 輪詢服務..."
	@GOTOOLCHAIN=local go run ${MAIN_PATH}

# 一鍵優雅關閉 (終止背景服務並清理所有 CAO/tmux Sessions)
stop:
	@echo "🛑 正在關閉 Agent Flow 與清理 CAO/tmux Sessions..."
	@cao shutdown 2>/dev/null || true
	@pkill -f "agent-flow" 2>/dev/null || true
	@echo "✅ 所有服務與 Sessions 已完成優雅關閉。"

# 編譯二進位執行檔
build:
	@echo "🔨 正在編譯 agent-flow..."
	@GOTOOLCHAIN=local go build -ldflags="-s -w" -o ${BINARY_NAME} ${MAIN_PATH}

# 查看目前活躍中的 CAO Sessions 狀態
cao-status:
	@cao session list

# 清理編譯檔與暫存檔
clean: stop
	@echo "🧹 清理編譯檔與暫存檔..."
	@rm -f ${BINARY_NAME}
	@echo "✅ 清理完成。"

fmt:
	@echo "🎨 格式化與靜態檢查程式碼..."
	@GOTOOLCHAIN=local go fmt ./...
	@GOTOOLCHAIN=local go vet ./...

test:
	@echo "🧪 執行全套單元測試..."
	@GOTOOLCHAIN=local go test -v ./...

# 以 Docker 建置映像 (UID/GID 與主機使用者一致)
docker-build:
	@AGENT_FLOW_UID=$$(id -u) AGENT_FLOW_GID=$$(id -g) docker compose build

# 以 Docker 在背景啟動 Agent Flow (含容器內 cao-server 與 CAO Sessions)
docker-up:
	@AGENT_FLOW_UID=$$(id -u) AGENT_FLOW_GID=$$(id -g) docker compose up -d --build

# 關閉容器 (agent-flow 會先清理容器內所有 CAO Sessions)
docker-down:
	@docker compose down

# 追蹤容器日誌
docker-logs:
	@docker compose logs -f
