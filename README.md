# Agent Flow：GitLab Todo 驅動的本地 AI 多代理編排器 (CAO 整合版)

[前置需求與 CAO 安裝](#前置需求與-cao-安裝) · [CAO Profiles 與 Skills 設定](#-cao-profiles-與-skills-設定指南) · [配置說明](#配置說明) · [快速啟動](#快速啟動) · [系統架構](#系統架構)

Agent Flow 是一個以 Go 語言開發的輕量級本地多代理編排器。它會輪詢 GitLab Merge Request 的 Todos，將工作直接派發給 AWS Labs `cli-agent-orchestrator` (CAO) 的 Supervisor 終端機進程，並以 GitLab 留言驗證任務結果。

---

## 🔗 前置需求與 CAO 安裝

本專案高度整合 AWS Labs 開源的 CLI Agent 編排工具 **[cli-agent-orchestrator (CAO)](https://github.com/awslabs/cli-agent-orchestrator)**。

### 1. 安裝 CLI Agent Orchestrator (CAO)

確保你的 Python 環境已就緒，隨後透過 `pip` 或 `pipx` 安裝 CAO：

```bash
# 使用 pipx (推薦)
pipx install cli-agent-orchestrator

# 或使用 pip
pip install cli-agent-orchestrator
```

驗證安裝：

```bash
cao --version
```

### 2. 啟動 CAO 背景服務

在啟動 Agent Flow 前，請確保 `cao-server` 已在背景運行：

```bash
cao-server &
```

---

## 🧩 CAO Profiles 與 Skills 設定指南

在 CAO 體系中，**Profile** 定義了 Agent 的角色與權限，而 **Skills** 為 Agent 提供了擴充的工具與工作流。

### 1. Agent Profiles 管理 (`cao profile`)

查看目前系統中已建立的 Agent Profiles：

```bash
cao profile list
```

建立或修改專屬 Agent Profile (例如 `review_supervisor` 或 `code_supervisor`)：

```bash
# 檢視特定 Profile 設定
cao profile show review_supervisor

# 建立自訂 Agent Profile (指定角色與工具權限)
cao profile create review_supervisor \
  --role "Reviewer Supervisor" \
  --allowed-tools "@builtin,fs_*,execute_bash"
```

建立完成後，即可在 `configs/config.yaml` 的 `cao_agent_profile` 欄位填入該 Profile 名稱。

### 2. Skills 技能管理 (`cao skills`)

CAO 支援透過 Skills 擴充 Agent 的自動化能力 (例如帶入特定的 Code Review 規範與專案範本)。

查看目前已安裝的 Skills 清單：

```bash
cao skills list
```

安裝新的 Skill (從本地目錄、Git URL 或遠端商店)：

```bash
# 從本地目錄安裝專案技能
cao skills install ./path/to/my-skill

# 從遠端 Git 儲存庫安裝技能
cao skills install https://github.com/user/my-agent-skill
```

---

## 🏗️ 系統架構

```mermaid
flowchart LR
  GitLab["GitLab Todos API"] --> AgentFlow["Agent Flow Daemon (Go)"]
  AgentFlow --> CAODispatcher["CAO Task Dispatcher"]
  CAODispatcher --> CAOServer["cao-server REST / CLI"]
  CAOServer --> Supervisor["CAO Supervisor Session (tmux)"]
```

---

## ⚙️ 配置說明

本專案採用 **完全配置驅動 (Config-Driven Architecture)**，無需修改任何程式碼或寫死 Bash 腳本即可自由定義 Agents、Session 名稱、Profile 與 AI Provider。

### 1. 複製設定檔範本

```bash
cp configs/config.yaml.example configs/config.yaml
```

### 2. 編輯 `configs/config.yaml`

```yaml
gitlab_url: "https://gitlab.your-company.com"
interval_seconds: 60
check_ci_success: true

# cao-server 連線位址
cao_server_url: "http://localhost:9889"

allowed_projects:
  - "group/project-a"
allowed_mr_authors:
  - "developer1"

# 自由定義 Agent 角色、專屬 Token、目標 Session 名稱、Agent Profile 與 Provider
agents:
  - id: "reviewer"
    gitlab_token: "glpat-reviewer-token-xxxxxx"
    cao_session_name: "gitlab-reviewer"         # 自訂 Session 名稱
    cao_agent_profile: "review_supervisor"      # 自訂 CAO Agent Profile
    cao_provider: "antigravity_cli"             # 自訂 Provider (相容 agy, kiro_cli, codex 等)

  - id: "coder"
    gitlab_token: "glpat-coder-token-yyyyyy"
    cao_session_name: "gitlab-coder"            # 自訂 Session 名稱
    cao_agent_profile: "code_supervisor"        # 自訂 CAO Agent Profile
    cao_provider: "codex"                       # 自訂 Provider
```

---

## 🚀 快速啟動

### 一鍵啟動 (One-Command Start)

```bash
make start
```

`make start` 會自動檢測 `cao-server` 狀態、依據 `config.yaml` 自動動態建立與對接相應的 CAO Sessions，並啟動背景輪詢服務。

### 一鍵優雅關閉 (Graceful Shutdown)

在終端機按下 `Ctrl+C` 或執行以下指令：

```bash
make stop
```

系統會自動停止輪詢，並同步深層清理背景所有 CAO/tmux Sessions 與資料庫殘留紀錄。

---

## 🛠️ 開發與常用指令

| 指令 | 說明 |
| :--- | :--- |
| **`make start`** | 自動診斷環境、自動建立 CAO Sessions 並啟動背景輪詢 |
| **`make stop`** | 優雅關閉輪詢服務並深層清理所有 CAO/tmux Sessions |
| **`make cao-status`** | 查看目前電腦中運作中的 CAO Sessions 狀態 |
| **`make test`** | 執行全套 Golang 單元測試 |
| **`make fmt`** | 執行程式碼格式化與靜態檢查 (`go fmt` + `go vet`) |
| **`make build`** | 編譯二進位執行檔 `agent-flow` |

---

## 📜 授權

請參閱 [LICENSE](LICENSE)。
