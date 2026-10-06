#!/usr/bin/env bash
set -e

# ==============================================================================
# Agent Flow 容器進入點：先確保容器內的 cao-server 運作，再啟動 agent-flow
# ==============================================================================

bash /usr/local/lib/agent-flow/bootstrap-cao.sh

# 以 exec 取代 shell，讓 SIGTERM 直接送達 agent-flow 以觸發 Session 清理
exec agent-flow "$@"
