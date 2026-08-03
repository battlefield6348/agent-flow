#!/usr/bin/env bash
set -e

# ==============================================================================
# CAO (CLI Agent Orchestrator) 本地環境診斷腳本
# ==============================================================================

echo "============================================================"
echo "🚀 正在檢查 Agent Flow 與 CAO 運行環境..."
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
    echo "⚠️ 偵測到 cao-server 未啟動，嘗試在背景自動啟動..."
    if command -v cao-server &> /dev/null; then
        nohup cao-server > /dev/null 2>&1 &
    elif command -v cao &> /dev/null; then
        nohup cao server > /dev/null 2>&1 &
    fi

    MAX_RETRIES=5
    RETRY_COUNT=0
    until curl -sf "$CAO_SERVER_URL/sessions" > /dev/null 2>&1 || [ $RETRY_COUNT -ge $MAX_RETRIES ]; do
        sleep 1
        RETRY_COUNT=$((RETRY_COUNT + 1))
    done

    if curl -sf "$CAO_SERVER_URL/sessions" > /dev/null 2>&1; then
        echo "✅ cao-server 已自動啟動完成！"
    else
        echo "⚠️ 警告: 自動啟動 cao-server 逾時或未成功 ($CAO_SERVER_URL)。"
        echo "💡 提示: 請手動檢查 'cao-server' 服務狀態。"
    fi
fi

echo "============================================================"
echo "🎉 環境診斷完成！所有 CAO Sessions 將由 configs/config.yaml 動態驅動建立。"
echo "============================================================"
