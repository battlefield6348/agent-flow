package orchestrator

// CollaboratorConfig 定義個案 Agent 輪詢與專屬派發設定
type CollaboratorConfig struct {
	ID             string `yaml:"id" json:"id"`
	GitLabToken    string `yaml:"gitlab_token" json:"gitlab_token"`
	CaoSessionName string `yaml:"cao_session_name" json:"cao_session_name"`
	PromptSuffix   string `yaml:"prompt_suffix" json:"prompt_suffix"`
}
