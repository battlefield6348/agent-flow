package orchestrator

import "testing"

func TestCleanLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Remove standard ANSI",
			input:    "\x1b[31mHello\x1b[0m",
			expected: "Hello",
		},
		{
			name:     "Remove carriage return",
			input:    "Old Line\rNew Line",
			expected: "New Line",
		},
		{
			name:     "Remove Braille patterns",
			input:    "Loading \u2801\u2802 Done",
			expected: "Loading  Done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanLine(tt.input)
			if got != tt.expected {
				t.Errorf("CleanLine() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCleanBlock(t *testing.T) {
	input := "First Part\n<thought>\nthinking...\n</thought>\nSecond Part"
	expected := "First Part\n\nSecond Part"
	got := CleanBlock(input)
	if got != expected {
		t.Errorf("CleanBlock() = %q, want %q", got, expected)
	}
}

func TestParseFinalResponse(t *testing.T) {
	input := "Step 1\n─────\nStep 2\n─────\nFinal Answer"
	expected := "Final Answer"
	got := ParseFinalResponse(input)
	if got != expected {
		t.Errorf("ParseFinalResponse() = %q, want %q", got, expected)
	}
}

func TestParseFinalResponse_PrefersStructuredReviewBlock(t *testing.T) {
	input := `› 請開始評審 Merge Request 32
• 使用 git-mr-workflow-reviewer 進行 MR 審查
## 審查結論
需修改後再審

## 必修問題
- migration 未同步
- 規格文件未更新

gpt-5.4 medium · ~/repo · 5h 82% left`

	expected := "## 審查結論\n需修改後再審\n\n## 必修問題\n- migration 未同步\n- 規格文件未更新"
	got := ParseFinalResponse(input)
	if got != expected {
		t.Errorf("ParseFinalResponse() = %q, want %q", got, expected)
	}
}

func TestParseFinalResponse_TrimsPromptTailAfterStructuredReview(t *testing.T) {
	input := `## 審查結論
需修改後再審
## 必修問題
- 描述格式不符
› Implement {feature}`

	expected := "## 審查結論\n需修改後再審\n## 必修問題\n- 描述格式不符"
	got := ParseFinalResponse(input)
	if got != expected {
		t.Errorf("ParseFinalResponse() = %q, want %q", got, expected)
	}
}

func TestShouldIgnore(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		ignore bool
	}{
		{
			name:   "Ignore TUI noise",
			input:  "────",
			ignore: true,
		},
		{
			name:   "Ignore status bar",
			input:  "Type your message",
			ignore: true,
		},
		{
			name:   "Keep valid answer",
			input:  "The result is 42",
			ignore: false,
		},
		{
			name:   "Ignore too short",
			input:  "a",
			ignore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldIgnore(tt.input); got != tt.ignore {
				t.Errorf("ShouldIgnore() = %v, want %v", got, tt.ignore)
			}
		})
	}
}
