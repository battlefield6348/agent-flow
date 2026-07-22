.PHONY: build start setup cao-status clean fmt test check-tools

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

# 編譯二進位執行檔
build:
	@echo "🔨 正在編譯 agent-flow..."
	@GOTOOLCHAIN=local go build -ldflags="-s -w" -o ${BINARY_NAME} ${MAIN_PATH}

# 查看目前活躍中的 CAO Sessions 狀態
cao-status:
	@cao session list

# 清理編譯檔與暫存檔
clean:
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
