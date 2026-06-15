package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type HttpGitLabRepository struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHttpGitLabRepository(baseURL, token string) *HttpGitLabRepository {
	return &HttpGitLabRepository{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchPendingTodos 抓取待處理的 Merge Request 待辦事項
func (r *HttpGitLabRepository) FetchPendingTodos(ctx context.Context) ([]Todo, error) {
	apiURL := fmt.Sprintf("%s/api/v4/todos?state=pending&type=MergeRequest&per_page=100", r.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab API returned status: %s", resp.Status)
	}

	var rawTodos []struct {
		ID         int    `json:"id"`
		ActionName string `json:"action_name"`
		TargetType string `json:"target_type"`
		Project    struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
		Target struct {
			IID         int    `json:"iid"`
			Title       string `json:"title"`
			Description string `json:"description"`
			SHA         string `json:"sha"`
			WebURL      string `json:"web_url"`
			State       string `json:"state"`
			Author      struct {
				Username string `json:"username"`
			} `json:"author"`
		} `json:"target"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rawTodos); err != nil {
		return nil, err
	}

	var todos []Todo
	for _, rt := range rawTodos {
		todos = append(todos, Todo{
			ID:         rt.ID,
			ActionName: rt.ActionName,
			TargetType: rt.TargetType,
			Project:    rt.Project.PathWithNamespace,
			MergeRequest: MergeRequest{
				IID:         rt.Target.IID,
				Title:       rt.Target.Title,
				Description: rt.Target.Description,
				SHA:         rt.Target.SHA,
				WebURL:      rt.Target.WebURL,
				State:       rt.Target.State,
				Author:      rt.Target.Author.Username,
			},
		})
	}

	return todos, nil
}

// MarkTodoAsDone 將 GitLab 上的特定待辦事項標記為已處理
func (r *HttpGitLabRepository) MarkTodoAsDone(ctx context.Context, todoID int) error {
	apiURL := fmt.Sprintf("%s/api/v4/todos/%d/mark_as_done", r.baseURL, todoID)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mark_as_done returned status: %s", resp.Status)
	}
	return nil
}

// GetUsername 取得當前 Token 對應的 GitLab 使用者名稱
func (r *HttpGitLabRepository) GetUsername(ctx context.Context) (string, error) {
	apiURL := fmt.Sprintf("%s/api/v4/user", r.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("PRIVATE-TOKEN", r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitLab User API status: %s", resp.Status)
	}

	var user struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}
	return user.Username, nil
}
