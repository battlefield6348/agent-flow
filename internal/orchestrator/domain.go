package orchestrator

// MergeRequest 代表 GitLab 中的合併請求領域模型
type MergeRequest struct {
	IID         int
	Title       string
	Description string
	SHA         string
	WebURL      string
	State       string
	Author      string
}

// Todo 代表 GitLab 中的待辦事項領域模型
type Todo struct {
	ID          int
	ActionName  string
	TargetType  string
	Project     string
	MergeRequest MergeRequest
}
