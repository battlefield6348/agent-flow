# 設計規格：Telegram AI 開發指揮部 (War Room Mode)

## 1. 核心願景
透過 Telegram 群組建立一個「人類 + 多 AI」的同步協作環境。使用者下達宏觀指令，AI 角色在群組內公開討論、規劃並執行，確保開發過程透明且可隨時介入。

## 2. 角色定義與標籤 (Tags)
- **#planner (Gemini)**: 負責架構設計、拆解任務 (Plan) 與代碼審查 (Review)。
- **#coder (Codex)**: 負責實作代碼、修復 Bug 與撰寫測試。
- **User (You)**: 負責設定目標、批准計畫、並在 Review 失敗時給予最終指導。

## 3. 協作流程 (Interaction Loop)
1. **指令發送**: 使用者在群組輸入：`#planner 請幫我設計一個分散式鎖的 Go 實作。`
2. **公開規劃**: Planner (Gemini) 收到後，在群組回覆：`收到，我的規劃如下：1. ... 2. ... #coder 請根據此規劃實作。`
3. **代碼實作**: Coder (Codex) 收到指令與規劃後，開始執行並在完成後回報：`實作完成，已提交至 internal/sync。#planner 請幫我 Review。`
4. **即時審查**: Planner (Gemini) 讀取代碼並在群組給予反饋：`Review 發現第 42 行有死鎖風險，請修正。`
5. **循環至完成**: 重複上述步驟直至使用者滿意。

## 4. 技術實作：IO 雙向綁定
- **Input (TG -> CLI)**: Bot 監控群組訊息，偵測到 `#tag` 時將文字發送到對應 Worker 的 `inputCh` (tmux)。
- **Output (CLI -> TG)**: Worker 的 `outputCallback` 會捕捉 tmux 螢幕上的新文字，並自動轉發回 Telegram 群組，並加上角色名稱首碼（如 `[Gemini]: ...`）。

## 5. 檔案結構調整
- `internal/telegram/bot.go`: 升級為支援群組模式與多 Agent 併發輸出。
- `cmd/agent-flow/main.go`: 修改為啟動 Telegram Bot 伺服器模式。
- `configs/config.yaml`: 增加 `telegram.chat_id` 配置以鎖定特定的指揮部群組。
