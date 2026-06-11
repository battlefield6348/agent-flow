package orchestrator

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	Logs struct {
		Path string `yaml:"path"`
	} `yaml:"logs"`
	Telegram struct {
		Token          string  `yaml:"token"`
		AllowedChatIDs []int64 `yaml:"allowed_chat_ids"`
	} `yaml:"telegram"`
	Scheduler struct {
		Enable          bool   `yaml:"enable"`
		IntervalSeconds int    `yaml:"interval_seconds"`
		GitLabURL       string `yaml:"gitlab_url"`
		ProjectPath     string `yaml:"project_path"`
		Username        string `yaml:"username"`
	} `yaml:"scheduler"`
	Collaborators []CollaboratorConfig `yaml:"collaborators"`
}

type CollaboratorConfig struct {
	ID                string            `yaml:"id"`
	Name              string            `yaml:"name"`
	Tag               string            `yaml:"tag"` // 用於 TG 的標籤，例如 #dev
	Cmd               string            `yaml:"cmd"`
	Args              []string          `yaml:"args"`
	Tags              []string          `yaml:"tags"`
	Skills            []string          `yaml:"skills"`
	Env               map[string]string `yaml:"env"`
	TGPrefix          string            `yaml:"tg_prefix"`
	InputPrefix       string            `yaml:"input_prefix"`
	OnlyFinalResponse bool              `yaml:"only_final_response"`
	Workspace         string            `yaml:"workspace"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
