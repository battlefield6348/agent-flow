.PHONY: build start stop status clean fmt test logs attach-r check-tools install-skills

# 基本變數設定
BINARY_NAME=collaborator
MAIN_PATH=./cmd/agent-flow/main.go

# 編譯執行檔
build:
	@echo "Building agent-flow..."
	go build -ldflags="-s -w" -o agent-flow ${MAIN_PATH}

# --- 核心操作指令 ---

# 一鍵啟動 (包含工具檢查)
start: check-tools
	@echo "Starting AI War Room (agent-flow)..."
	@go run ${MAIN_PATH}

# 一鍵停止 (優雅終止所有進程與 Session)
stop:
	@echo "Stopping all AI services..."
	@# 先優雅關閉 tmux session
	@tmux kill-session -t reviewer 2>/dev/null || true
	@tmux kill-session -t coder 2>/dev/null || true
	@# 再精確殺掉以目前專案目錄運行的 Go 執行檔與孤兒進程
	@pkill -x agent-flow 2>/dev/null || true
	@ps aux | grep "go run ./cmd/agent-flow/main.go" | grep -v grep | awk '{print $$2}' | xargs -r kill -9 || true
	@lsof +D . 2>/dev/null | grep -E '\bmain\b' | awk '{print $$2}' | xargs -r kill -9 || true
	@echo "All services stopped."

# 清理環境 (日誌、暫存檔與編譯檔)
clean: stop
	@echo "Cleaning up logs and temporary files..."
	@rm -rf logs/*
	@rm -f plan.json error_log.json agent-flow ${BINARY_NAME}
	@echo "Cleaned."

# --- 監看指令 ---

# 同時監看所有 AI 的即時日誌 (Tail)
logs:
	@echo "Tailing all AI logs (Ctrl+C to stop)..."
	@tail -f logs/*.log

# 進入 Reviewer 現場 (tmux)
attach-r:
	@echo "TIP: Press 'Ctrl+b' then 'd' to exit WITHOUT killing the AI."
	@sleep 2
	@tmux attach -t reviewer

# 查看運行狀態
status:
	@echo "Current AI Sessions:"
	@echo "----------------------------------------------------"
	@tmux ls 2>/dev/null | grep -E 'reviewer|coder' || echo "All AI Workers are OFFLINE."
	@echo "----------------------------------------------------"

# --- 輔助指令 ---

# 安裝 skills：把 repo 內 skills/ 下每個技能 symlink 到「各 CLI 對應的 skill 目錄」。
# repo 是唯一事實來源，symlink 讓 repo 一改即生效；換機/新成員設定時務必先跑一次。
# 目錄依 CLI 而異：claude/codex 用中性的 ~/.agent-flow/skills；agy(Antigravity)原生
# /技能名 只認 ~/.gemini/antigravity/skills。一次安裝到兩處，成員不管用哪個 CLI 都吃得到。
# 日後要支援新的 agent，只要在 SKILLS_DESTS 加一個目錄即可。
SKILLS_SRC := $(CURDIR)/skills
SKILLS_DESTS := $(HOME)/.agent-flow/skills $(HOME)/.gemini/antigravity/skills

install-skills:
	@for dest in $(SKILLS_DESTS); do \
		mkdir -p "$$dest"; \
		for d in $(SKILLS_SRC)/*/; do \
			[ -e "$$d" ] || continue; \
			name=$$(basename "$$d"); \
			rm -rf "$$dest/$$name"; \
			ln -sfn "$$d" "$$dest/$$name"; \
			echo "linked skill: $$name -> $$dest/$$name"; \
		done; \
	done
	@echo "Skills installed to: $(SKILLS_DESTS)"

check-tools:
	@command -v tmux >/dev/null 2>&1 || { echo >&2 "Error: tmux is not installed."; exit 1; }
	@if [ ! -f configs/config.yaml ]; then \
		echo >&2 "Error: configs/config.yaml not found. Please run: cp configs/config.yaml.example configs/config.yaml and set the cmd field(s) for your collaborators."; \
		exit 1; \
	fi
	@for tool in $$(grep -E '^\s*cmd:\s*"?[^"#]+' configs/config.yaml | sed -E 's/^\s*cmd:\s*"?//; s/"?\s*$$//'); do \
		command -v "$$tool" >/dev/null 2>&1 || { echo >&2 "Error: $$tool cli (from configs/config.yaml cmd field) is not installed."; exit 1; }; \
	done
	@echo "Environment check PASSED."

fmt:
	@echo "Formatting code..."
	go fmt ./...
	go vet ./...

test:
	@echo "Running tests..."
	go test -v ./...
