package orchestrator

import (
	"fmt"
	"strings"
)

const (
	ModeMultiSession = "multi_session"
	ModeSupervisor   = "supervisor"
)

const (
	RoleReviewer = "reviewer"
	RoleCoder    = "coder"
)

// CollaboratorConfig 定義個案 Agent 輪詢與專屬 CAO Session 配置
type CollaboratorConfig struct {
	ID              string `yaml:"id" json:"id"`
	Role            string `yaml:"role" json:"role"`
	Replicas        int    `yaml:"replicas" json:"replicas"`
	GitLabToken     string `yaml:"gitlab_token" json:"gitlab_token"`
	CaoSessionName  string `yaml:"cao_session_name" json:"cao_session_name"`
	CaoAgentProfile string `yaml:"cao_agent_profile" json:"cao_agent_profile"`
	CaoProvider     string `yaml:"cao_provider" json:"cao_provider"`
	PromptSuffix    string `yaml:"prompt_suffix" json:"prompt_suffix"`
}

// EffectiveRole 未設定 role 時沿用 id，讓既有以 id: reviewer / coder 區分角色的設定檔不需修改
func (c CollaboratorConfig) EffectiveRole() string {
	if role := strings.ToLower(strings.TrimSpace(c.Role)); role != "" {
		return role
	}
	return c.ID
}

// PoolSize 回傳實際會建立的 Session 數量
func (c CollaboratorConfig) PoolSize() int {
	// 多個 coder 會在同一個本機 Repo 目錄互相覆寫修改，需先有 worktree 隔離才能平行
	if c.Replicas <= 1 || c.EffectiveRole() == RoleCoder {
		return 1
	}
	return c.Replicas
}

// SessionNames 回傳此 Agent 擁有的 CAO Session 名稱；單一 Session 時沿用原名稱，避免既有部署的 Session 被重建
func (c CollaboratorConfig) SessionNames() []string {
	base := c.CaoSessionName
	if base == "" {
		base = fmt.Sprintf("gitlab-%s", c.ID)
	}
	size := c.PoolSize()
	if size == 1 {
		return []string{formatSessionName(base)}
	}
	names := make([]string, 0, size)
	for i := 1; i <= size; i++ {
		names = append(names, formatSessionName(fmt.Sprintf("%s-%d", base, i)))
	}
	return names
}
