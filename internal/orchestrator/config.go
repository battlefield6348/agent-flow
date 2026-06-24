package orchestrator

import (
	"errors"
	"fmt"
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
		CheckCISuccess   *bool    `yaml:"check_ci_success"`
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
	GitLabToken       string   `yaml:"gitlab_token"`
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

	config.applyDefaults()
	return &config, nil
}

func (c *Config) applyDefaults() {
	if c.Logs.Path == "" {
		c.Logs.Path = "./logs"
	}
	if c.Scheduler.IntervalSeconds <= 0 {
		c.Scheduler.IntervalSeconds = 60
	}
	if c.Scheduler.GitLabURL == "" {
		c.Scheduler.GitLabURL = "https://git.efaipd.com"
	}
	if c.Scheduler.CheckCISuccess == nil {
		defaultVal := true
		c.Scheduler.CheckCISuccess = &defaultVal
	}
}

func (c *Config) Validate() error {
	if len(c.Collaborators) == 0 {
		return errors.New("at least one collaborator must be configured")
	}
	for _, col := range c.Collaborators {
		if col.ID == "" {
			return errors.New("collaborator ID cannot be empty")
		}
		if col.Cmd == "" {
			return fmt.Errorf("collaborator %s command (cmd) cannot be empty", col.ID)
		}
		if col.GitLabToken == "" {
			return fmt.Errorf("collaborator %s: gitlab_token cannot be empty (Strict Mode)", col.ID)
		}
	}
	return nil
}
