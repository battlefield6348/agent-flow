# agent-flow: AI 協作編排工具

`agent-flow` 是一個基於 Go 實作的本地編排器 (Orchestrator)，專門用於協調多個 **Gemini CLI** (Workers) 在本地執行自動化開發與評審任務。

## 核心功能
1.  **多角色協作 (Worker Manager)**：透過 `tmux` 同時管理多個具備不同能力的 AI 實體（如 `coder`、`reviewer`）。
2.  **GitLab 排程監聽 (Scheduler)**：自動監聽 GitLab 上的 Merge Requests (MR)，尋找標註 `#reviewer` 或直接指派給使用者的任務。
3.  **自動環境切換**：當 Scheduler 發現任務時，會自動解析專案路徑並將本地 Worker 切換到對應的代碼工作空間。

## 快速啟動
1.  **配置環境**：
    複製 `configs/config.yaml.example` 並重新命名為 `configs/config.yaml`，填入您的 GitLab URL 與 Worker 設定。
2.  **啟動服務**：
    ```bash
    make start
    ```
    這會啟動背景 Workers 以及 GitLab 監聽排程。

## 指令集 (Makefile)
- `make start`: 啟動服務。
- `make stop`: 停止所有 AI 服務與 tmux 會話。
- `make status`: 查看當前 AI 運行狀態。
- `make logs`: 監看 AI 的即時輸出。
- `make attach-r`: 進入 Reviewer 的現場 (`tmux attach`)。
- `make attach-c`: 進入 Coder 的現場 (`tmux attach`)。

## 專案結構
- `cmd/agent-flow`: 核心啟動邏輯與 GitLab 監聽程序。
- `internal/orchestrator`: Worker 管理與配置載入。
- `configs/config.yaml`: 系統設定檔。
- `logs/`: 存放 AI 的對話記錄與執行結果。
