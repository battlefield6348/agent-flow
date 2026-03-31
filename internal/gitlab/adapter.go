package gitlab

import (
	"fmt"
	"github.com/xanzy/go-gitlab"
)

type Adapter struct {
	client *gitlab.Client
}

func NewAdapter(baseURL string, token string) (*Adapter, error) {
	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to create gitlab client: %w", err)
	}
	return &Adapter{client: client}, nil
}

// GetMRPipelineStatus 獲取特定 MR 的 Pipeline 完整狀態
func (a *Adapter) GetMRPipelineStatus(projectID interface{}, mrIID int) (string, error) {
	pipelines, _, err := a.client.MergeRequests.ListMergeRequestPipelines(projectID, mrIID, nil)
	if err != nil {
		return "", err
	}
	if len(pipelines) == 0 {
		return "none", nil
	}
	// 返回最新的一個 pipeline 狀態 (如 success, failed, running)
	return pipelines[0].Status, nil
}

// ListProjectMRs 查詢符合特定標籤或狀態的 MR 清單
func (a *Adapter) ListProjectMRs(projectID interface{}, state string, labels []string) ([]*gitlab.MergeRequest, error) {
	opt := &gitlab.ListProjectMergeRequestsOptions{
		State: gitlab.Ptr(state),
	}
	if len(labels) > 0 {
		l := gitlab.LabelOptions(labels)
		opt.Labels = &l
	}
	mrs, _, err := a.client.MergeRequests.ListProjectMergeRequests(projectID, opt)
	return mrs, err
}

// PostMRComment 在 MR 下方留言彙報進度
func (a *Adapter) PostMRComment(projectID interface{}, mrIID int, body string) error {
	opt := &gitlab.CreateMergeRequestNoteOptions{
		Body: gitlab.Ptr(body),
	}
	_, _, err := a.client.Notes.CreateMergeRequestNote(projectID, mrIID, opt)
	return err
}
