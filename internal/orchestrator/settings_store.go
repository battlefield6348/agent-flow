package orchestrator

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type WorkflowSettings struct {
	GitLabURL        string               `yaml:"gitlab_url" json:"gitlab_url"`
	IntervalSeconds  int                  `yaml:"interval_seconds" json:"interval_seconds"`
	AllowedProjects  []string             `yaml:"allowed_projects" json:"allowed_projects"`
	AllowedMRAuthors []string             `yaml:"allowed_mr_authors" json:"allowed_mr_authors"`
	Agents           []CollaboratorConfig `yaml:"agents" json:"-"`
}

func LoadWorkflowSettings(path string) (WorkflowSettings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return WorkflowSettings{}, nil
	}
	if err != nil {
		return WorkflowSettings{}, err
	}
	var settings WorkflowSettings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return WorkflowSettings{}, err
	}
	return settings, nil
}

func SaveWorkflowSettings(path string, settings WorkflowSettings) error {
	data, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
