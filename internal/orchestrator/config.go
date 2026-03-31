package orchestrator

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GitLab struct {
		BaseURL      string `yaml:"base_url"`
		Token        string `yaml:"token"`
		ProjectID    string `yaml:"project_id"`
		PollInterval string `yaml:"poll_interval"`
	} `yaml:"gitlab"`
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	Collaborators []CollaboratorConfig `yaml:"collaborators"`
}

type CollaboratorConfig struct {
	ID     string            `yaml:"id"`
	Name   string            `yaml:"name"`
	Cmd    string            `yaml:"cmd"`
	Args   []string          `yaml:"args"`
	Tags   []string          `yaml:"tags"`
	Skills []string          `yaml:"skills"` // 指定此 Agent 具備哪些專屬技能
	Env    map[string]string `yaml:"env"`
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
