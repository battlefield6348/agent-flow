.PHONY: build run clean fmt test

# 基本變數設定
BINARY_NAME=collaborator
MAIN_PATH=./cmd/collaborator/main.go

# 編譯執行檔 (優化二進位大小)
build:
	@echo "Building ${BINARY_NAME}..."
	go build -ldflags="-s -w" -o ${BINARY_NAME} ${MAIN_PATH}

# 直接執行專案 (開發常用)
run:
	@echo "Starting Orchestrator..."
	go run ${MAIN_PATH}

# 清理專案 (移除編譯檔與產出的日誌)
clean:
	@echo "Cleaning up..."
	rm -f ${BINARY_NAME}
	rm -f *.log

# 代碼美化與靜態檢查 (符合專案規範)
fmt:
	@echo "Formatting code..."
	go fmt ./...
	go vet ./...

# 執行單元測試
test:
	@echo "Running tests..."
	go test -v ./...
