package orchestrator

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Logs struct {
		Path string `yaml:"path"`
	} `yaml:"logs"`
	Scheduler struct {
		IntervalSeconds  int      `yaml:"interval_seconds"`
		GitLabURL        string   `yaml:"gitlab_url"`
		AllowedProjects  []string `yaml:"allowed_projects"`
		AllowedMRAuthors []string `yaml:"allowed_mr_authors"`
	} `yaml:"scheduler"`
	Collaborators []CollaboratorConfig `yaml:"collaborators"`
}

type CollaboratorConfig struct {
	ID                string   `yaml:"id"`
	Name              string   `yaml:"name"`
	Cmd               string   `yaml:"cmd"`
	Args              []string `yaml:"args"`
	Skills            []string `yaml:"skills"`
	InputPrefix       string   `yaml:"input_prefix"`
	OnlyFinalResponse bool     `yaml:"only_final_response"`
	Workspace         string   `yaml:"workspace"`
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
