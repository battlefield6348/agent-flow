package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Client 是對 GitLab API 的封裝
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// TodoDTO 對應 GitLab API 的 Todo 結構
type TodoDTO struct {
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

// UserDTO 對應 GitLab API 的 User 結構
type UserDTO struct {
	Username string `json:"username"`
}

type NoteDTO struct {
	ID        int       `json:"id"`
	Body      string    `json:"body"`
	System    bool      `json:"system"`
	CreatedAt time.Time `json:"created_at"`
	Author    struct {
		Username string `json:"username"`
	} `json:"author"`
}

// FetchPendingTodos 呼叫 GitLab API 取得待處理的待辦事項
func (c *Client) FetchPendingTodos(ctx context.Context) ([]TodoDTO, error) {
	apiURL := fmt.Sprintf("%s/api/v4/todos?state=pending&type=MergeRequest&per_page=100", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab API returned status: %s", resp.Status)
	}

	var todos []TodoDTO
	if err := json.NewDecoder(resp.Body).Decode(&todos); err != nil {
		return nil, err
	}
	return todos, nil
}

// MarkTodoAsDone 呼叫 GitLab API 將待辦事項標記為完成
func (c *Client) MarkTodoAsDone(ctx context.Context, todoID int) error {
	apiURL := fmt.Sprintf("%s/api/v4/todos/%d/mark_as_done", c.baseURL, todoID)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mark_as_done returned status: %s", resp.Status)
	}
	return nil
}

// FetchMergeRequestNotes 呼叫 GitLab API 取得指定 MR 的留言列表
func (c *Client) FetchMergeRequestNotes(ctx context.Context, projectPath string, mrIID int) ([]NoteDTO, error) {
	encodedPath := url.PathEscape(projectPath)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/notes?per_page=100", c.baseURL, encodedPath, mrIID)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch MR notes returned status: %s", resp.Status)
	}

	var notes []NoteDTO
	if err := json.NewDecoder(resp.Body).Decode(&notes); err != nil {
		return nil, err
	}
	return notes, nil
}

// GetCurrentUser 呼叫 GitLab API 取得當前使用者資訊
func (c *Client) GetCurrentUser(ctx context.Context) (*UserDTO, error) {
	apiURL := fmt.Sprintf("%s/api/v4/user", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab User API status: %s", resp.Status)
	}

	var user UserDTO
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

type PipelineDTO struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
}

func (c *Client) FetchMergeRequestPipelines(ctx context.Context, projectPath string, mrIID int) ([]PipelineDTO, error) {
	encodedPath := url.PathEscape(projectPath)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/pipelines", c.baseURL, encodedPath, mrIID)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab API returned status: %s", resp.Status)
	}

	var pipelines []PipelineDTO
	if err := json.NewDecoder(resp.Body).Decode(&pipelines); err != nil {
		return nil, err
	}
	return pipelines, nil
}
