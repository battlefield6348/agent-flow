package orchestrator

// CollaboratorConfig 定義個案 Agent 輪詢與專屬 CAO Session 配置
type CollaboratorConfig struct {
	ID              string `yaml:"id" json:"id"`
	GitLabToken     string `yaml:"gitlab_token" json:"gitlab_token"`
	CaoSessionName  string `yaml:"cao_session_name" json:"cao_session_name"`
	CaoAgentProfile string `yaml:"cao_agent_profile" json:"cao_agent_profile"`
	CaoProvider     string `yaml:"cao_provider" json:"cao_provider"`
	PromptSuffix    string `yaml:"prompt_suffix" json:"prompt_suffix"`
}
