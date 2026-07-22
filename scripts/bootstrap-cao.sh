#!/usr/bin/env bash
set -e

# ==============================================================================
# CAO (CLI Agent Orchestrator) 環境初始化與 Session 自動建立腳本
# ==============================================================================

echo "============================================================"
echo "🚀 開始初始化 Agent Flow 與 CAO (CLI Agent Orchestrator) 環境..."
echo "============================================================"

# 1. 檢查 cao CLI 工具是否已安裝
if ! command -v cao &> /dev/null; then
    echo "❌ 錯誤: 系統找不到 'cao' 命令列工具，請先安裝 cli-agent-orchestrator。"
    exit 1
fi
echo "✅ 已偵測到 cao CLI 工具: $(which cao)"

# 2. 檢查 cao-server 是否在線 (預設位址: http://localhost:9889)
CAO_SERVER_URL="${CAO_SERVER_URL:-http://localhost:9889}"
echo "🔍 檢查 cao-server 連線狀態 ($CAO_SERVER_URL)..."
if curl -sf "$CAO_SERVER_URL/sessions" > /dev/null 2>&1; then
    echo "✅ cao-server 已在背景正常運作中！"
else
    echo "⚠️ 警告: 未能連線至 cao-server ($CAO_SERVER_URL)。"
    echo "💡 提示: 請確保在獨立終端機啟動過 'cao-server' 服務。"
fi

# 3. 自動檢查與建立目標 CAO Sessions
REVIEWER_SESSION="${CAO_REVIEWER_SESSION:-cao-reviewer-session}"
CODER_SESSION="${CAO_CODER_SESSION:-cao-coder-session}"

ACTIVE_SESSIONS=$(cao session list 2>/dev/null || true)

# 初始化 Reviewer Session
if echo "$ACTIVE_SESSIONS" | grep -q "$REVIEWER_SESSION"; then
    echo "✅ 發現已存在 Reviewer Session: $REVIEWER_SESSION"
else
    echo "🔨 正在初始化 Reviewer CAO Session: $REVIEWER_SESSION..."
    cao launch --agents review_supervisor --session-name "$REVIEWER_SESSION" --headless --auto-approve || true
fi

# 初始化 Coder Session
if echo "$ACTIVE_SESSIONS" | grep -q "$CODER_SESSION"; then
    echo "✅ 發現已存在 Coder Session: $CODER_SESSION"
else
    echo "🔨 正在初始化 Coder CAO Session: $CODER_SESSION..."
    cao launch --agents code_supervisor --session-name "$CODER_SESSION" --headless --auto-approve || true
fi

echo "============================================================"
echo "🎉 CAO 環境與 Sessions 初始化完畢！Ready to run Agent Flow."
echo "============================================================"
