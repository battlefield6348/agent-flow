package orchestrator

import (
	"context"
	"gemini-collaborator-go/internal/gitlab"
)

type HttpGitLabRepository struct {
	client *gitlab.Client
}

func NewHttpGitLabRepository(baseURL, token string) *HttpGitLabRepository {
	return &HttpGitLabRepository{
		client: gitlab.NewClient(baseURL, token),
	}
}

// FetchPendingTodos 抓取待處理的 Merge Request 待辦事項，並將 GitLab DTO 轉換為領域物件
func (r *HttpGitLabRepository) FetchPendingTodos(ctx context.Context) ([]Todo, error) {
	rawTodos, err := r.client.FetchPendingTodos(ctx)
	if err != nil {
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
	return r.client.MarkTodoAsDone(ctx, todoID)
}

// GetUsername 取得當前 Token 對應的 GitLab 使用者名稱
func (r *HttpGitLabRepository) GetUsername(ctx context.Context) (string, error) {
	user, err := r.client.GetCurrentUser(ctx)
	if err != nil {
		return "", err
	}
	return user.Username, nil
}

// FetchMergeRequestPipelines 取得特定 MR 的 Pipeline 狀態列表
func (r *HttpGitLabRepository) FetchMergeRequestPipelines(ctx context.Context, projectPath string, mrIID int) ([]Pipeline, error) {
	rawPipelines, err := r.client.FetchMergeRequestPipelines(ctx, projectPath, mrIID)
	if err != nil {
		return nil, err
	}

	var pipelines []Pipeline
	for _, rp := range rawPipelines {
		pipelines = append(pipelines, Pipeline{
			ID:     rp.ID,
			Status: rp.Status,
		})
	}
	return pipelines, nil
}

