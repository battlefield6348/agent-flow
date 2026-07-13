# AGENTS.md — agent-flow 共通規範

本檔是 `agent-flow` 專案對所有 AI agent 共用的主規範。無論使用 Codex、
Claude Code、Antigravity (`agy`/Gemini) 或其他 CLI，都應先讀本檔。

Go 程式撰寫規範請看 [DEVELOPMENT.md](DEVELOPMENT.md)。
設定檔欄位與不同 CLI 的建議值請看
[configs/config.yaml.example](configs/config.yaml.example)。

## 專案是什麼

`agent-flow` 是一個本地多 agent orchestrator：

- 透過 GitLab Todos API 找出待辦 Merge Request
- 透過 `tmux` 管理本地 AI CLI worker
- 依 MR 所屬專案切換對應 workspace
- 在淨化過的環境變數中執行 review 或開發任務

核心程式位於：

- `cmd/agent-flow`
- `internal/orchestrator`
- `internal/gitlab`

## 快速啟動與常用命令

安裝、啟動與 `make` 命令清單見 [README.md](README.md#快速開始)。

## 觸發機制

系統只透過 GitLab Todos API 得知有沒有工作可做。

1. 每輪用該 collaborator 的 `gitlab_token` 呼叫
   `GET /api/v4/todos?state=pending&type=MergeRequest`
2. 此 API 只會回傳「該 token 所屬帳號自己的待辦」
3. GitLab 只會在「別人對你做動作」時建立 todo
4. bot 用自己的 token 把自己設為 reviewer，不會建立 todo

## 派工條件

掃到 todo 後會依序過濾：

1. MR 狀態必須是 `opened`
2. 專案必須通過 `allowed_projects`
3. 作者必須通過 `allowed_mr_authors`
4. 若 `check_ci_success=true`，最新 pipeline 必須是 `success`
5. 若 worker 正忙，該 MR 延後到下一輪

補充：

- 若 MR 完全沒有 pipeline，CI 檢查視為放行
- 非 `opened` 的 MR todo 會被清掉

## Todo 何時清掉

目前行為是：

- MR 只是被派工時，**不會**立刻標記 todo done
- 只有 worker 產出有效結果後，才會把該 todo 標記為 done
- 若 worker 卡在 `session limit` 或只產生無效終端輸出，todo 會保留為 pending

這代表：

- 若要重跑一筆已成功完成的 review，仍需重新 @bot 或重新指定 reviewer
- 若某筆 pending todo 還在，通常表示尚未成功完成有效處理

## 如何手動觸發一次 review

1. 確認 bot token 對目標專案有讀取權限
2. 確認該專案與作者通過白名單，或白名單為空
3. 由 MR 作者或其他人把 bot 加成 reviewer，或在 MR 留言 `@bot`
4. 若有開 CI gate，等最新 pipeline 成功
5. 等下一輪輪詢

## 白名單比對規則

- `allowed_projects`：比對 GitLab `path_with_namespace`
- `allowed_mr_authors`：比對 GitLab `username`
- 兩者都用「轉小寫、去頭尾空白後完全相等」
- `[]` 代表不過濾

## 多 CLI 整合原則

實際的 `cmd`、`args`、`skills`、`input_prefix`、`prompt_suffix` 建議值請看
[configs/config.yaml.example](configs/config.yaml.example)。

這裡只列共通原則：

- 每個 collaborator 都是獨立 CLI 進程
- 每個 collaborator 都應有自己的 `gitlab_token`
- `skills` 只有在本機技能目錄真的存在時才會載入
- workspace 必須是本機實際存在的絕對路徑

## 技能注入機制

`collaborators[].skills` 對應本機目錄，且**依 CLI 而異**：

- `claude` / `codex`：`~/.agent-flow/skills/<skill-name>/`（中性路徑，靠 `--add-dir` 掛入）
- `agy`：`~/.gemini/antigravity/skills/<skill-name>/`（Antigravity 原生 `/技能名` 只認此目錄）

`make install-skills` 會一次把 repo `skills/` symlink 到上述所有目錄，故三種 CLI 共用同一份。

行為如下：

- 目錄存在才載入，不存在就略過
- `agy`：使用原生 `/技能名`
- `claude` / `codex`：先透過 `--add-dir` 掛入技能目錄，再用自然語言要求讀取
  `SKILL.md`

## Worker 就緒偵測

Worker 會透過 tmux 畫面判定 CLI 是否可接收輸入。

目前要點：

- 支援 Claude Code 的 `❯` 提示符
- 會排除確認對話框與選單，避免誤把選單當成 ready
- 畫面出現 `thinking` / `queued` / `working` 視為仍在執行

## MR 描述 / review 格式

一致性靠 skill 內容決定，skill 已納入 repo 的 `skills/`，為唯一事實來源：

- `git-mr-workflow-reviewer`
- `git-mr-workflow-coder`

換機/新成員用 `make install-skills` 把它們 symlink 到各 CLI 對應目錄（見「技能注入機制」）。

> **為什麼把 skill 放進 repo：** 早期 skill 只存在單一開發者的 `~/.gemini/...`，換機、重灌或
> 換成員時這些檔案不會跟著走，reviewer/coder 就退化成沒有工作流程的裸 CLI、留言格式全亂。
> 納入 repo + `make install-skills` 是為了消除這個「換機就走鐘」的風險，讓行為可攜、可重現。

留言鐵則（reviewer 與 coder 皆適用，跨 agent 公約）：

- **留言內文只能是該角色的結構化產出**（reviewer 的 `## 審查結論` 四段、coder 的 `## 修正回覆`）。
  第一個字元就是該標題，絕不夾帶 CLI 的 TUI 畫面、外框線、啟動橫幅、狀態列、MCP 警告、
  思考過程或操作 transcript。
- **一律用 `glab` CLI 貼文**（採用注入的 `GITLAB_TOKEN`），不要用綁死特定帳號的 GitLab MCP，
  以免留言掛錯身分。

這代表：

- skill 未安裝（沒跑 `make install-skills`）時格式無法保證一致
- repo 內目前沒有 CI 檢查來強制修正格式

## 多專案 / 多 session 限制

tmux session 名稱直接使用 collaborator `id`。

因此：

- 多個專案若同時跑 `agent-flow`
- 且使用相同 `id`（例如都叫 `reviewer`）

後啟動者會覆蓋先啟動者的 tmux session。

解法只有一個：自行使用不同 `id`，例如：

- `reviewer-admin-api`
- `reviewer-app-api`

## 已知限制

1. MR 描述與 review 格式一致性依賴 skill 有被安裝（`make install-skills`），repo 無 CI 強制
2. tmux session 仍無跨進程鎖
3. `config.yaml`（含 token 與絕對路徑）不在 repo，換機須重建；詳見 README「換機 / 新機器設定」
