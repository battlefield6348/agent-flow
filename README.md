# agent-flow: AI 協作編排工具

`agent-flow` 是一個基於 Go 實作的本地 Orchestrator，專門用於協調 **Gemini CLI** 與 **Codex CLI** 進行自動化開發。

## 核心工作流 (The Loop)
1. **Gemini (Planner)**: 解析 `task.json` 並產出具體的 `plan.json`。
2. **Codex (Executor)**: 根據 `plan.json` 進行代碼實作。
3. **Validator**: 執行 `go test`。若失敗，將錯誤日誌餵回給 Gemini 重新規劃。

## 環境設定
請確保系統中已安裝 `gemini` 與 `codex` 指令，並設定相關環境變數：
- `GEMINI_API_KEY`: 您的 Gemini API 金鑰。
- `CODEX_PATH`: (選擇性) Codex 執行檔路徑。

## 使用方式
1. 準備 `task.json`:
   ```json
   {
     "id": "TASK-001",
     "description": "在 internal/math 目錄下新增一個 Add 函數，並撰寫對應測試。"
   }
   ```
2. 啟動任務:
   ```bash
   go run cmd/agent-flow/main.go --task=task.json
   ```

## 檔案說明
- `task.json`: 原始任務描述。
- `plan.json`: Gemini 產出的執行計畫。
- `error_log.json`: 當步驟失敗時記錄的 stderr 與詳細錯誤。
