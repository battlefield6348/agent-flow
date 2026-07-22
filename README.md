# Agent Flow：GitLab Todo 驅動的本地 AI 多代理編排器 (CAO 整合版)

[快速啟動](#快速啟動) · [架構說明](#架構說明) · [開發與測試](#開發與測試)

Agent Flow 是一個以 Go 語言開發的輕量級本地多代理編排器。它會輪詢 GitLab Merge Request 的 Todos，將工作直接派發給 AWS Labs `cli-agent-orchestrator` (CAO) 的 Supervisor 終端機進程，並以 GitLab 留言驗證任務結果。

---

## 🌟 核心特色

- **GitLab Todo 自動驅動**：自動掃描 GitLab 待辦事項，派發特定 MR 評審與修復任務。
- **無縫整合 CAO (cli-agent-orchestrator)**：直連本地 CAO Supervisor 編排大腦，完美相容 `antigravity_cli` (`agy`), `kiro_cli`, `codex`, `claude_code`, `cursor_cli` 等 Multi-agent 工具。
- **完全配置驅動 (Config-Driven)**：零寫死腳本！在 `configs/config.yaml` 靈活自訂各 Agent 的專屬 Session 名稱、Agent Profile 與 Provider。
- **雙重優雅清理 (Graceful Shutdown)**：支援 `make stop` 與按 `Ctrl+C` 時自動深層清理背景 tmux 與 CAO 資料庫紀錄，防止資源殘留與競態衝突。

---

## 🏗️ 系統架構

```mermaid
flowchart LR
  GitLab[GitLab Todos API] --> AgentFlow[Agent Flow Daemon (Go)]
  AgentFlow --> CAODispatcher[CAO Task Dispatcher]
  CAODispatcher --> CAOServer[cao-server REST / CLI]
  CAOServer --> Supervisor[CAO Supervisor Session (tmux)]
```

---

## 🚀 快速啟動

### 1. 編輯設定檔

複製範例設定檔：

```bash
cp configs/config.yaml.example configs/config.yaml
```

編輯 `configs/config.yaml` 填入你的 GitLab URL 與專屬 Agents 配置：

```yaml
gitlab_url: "https://gitlab.your-company.com"
interval_seconds: 60
check_ci_success: true

cao_server_url: "http://localhost:9889"

agents:
  - id: "reviewer"
    gitlab_token: "glpat-reviewer-token-xxxxxx"
    cao_session_name: "gitlab-reviewer"
    cao_agent_profile: "review_supervisor"
    cao_provider: "antigravity_cli"

  - id: "coder"
    gitlab_token: "glpat-coder-token-yyyyyy"
    cao_session_name: "gitlab-coder"
    cao_agent_profile: "code_supervisor"
    cao_provider: "codex"
```

### 2. 一鍵啟動

```bash
make start
```

`make start` 會自動檢查 `cao-server` 狀態、依據 `config.yaml` 自動建立/對接必要的 CAO Sessions，並啟動 `agent-flow` 背景輪詢服務！

### 3. 一鍵優雅關閉

在終端機按下 `Ctrl+C`，或執行：

```bash
make stop
```

即可優雅停止輪詢並自動清理所有背景 `tmux` 與 CAO 暫存 Sessions。

---

## 🛠️ 開發與測試

```bash
# 執行全套單元測試
make test

# 程式碼格式化與靜態檢查
make fmt

# 編譯執行檔
make build
```

---

## 📜 授權

請參閱 [LICENSE](LICENSE)。
