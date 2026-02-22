package plan

type QuestionType string

const (
	QuestionTypeClarification QuestionType = "clarification"
	QuestionTypeScope         QuestionType = "scope"
	QuestionTypeCriteria      QuestionType = "criteria"
	QuestionTypeResearch      QuestionType = "research"
)

type Question struct {
	ID      string       `json:"id"`
	Type    QuestionType `json:"type"`
	Prompt  string       `json:"prompt"`
	Context string       `json:"context,omitempty"`
	Options []string     `json:"options,omitempty"`
}

type ResponseType string

const (
	ResponseTypeAnswer ResponseType = "answer"
	ResponseTypeSkip   ResponseType = "skip"
	ResponseTypeDefer  ResponseType = "defer"
)

type Answer struct {
	QuestionID string       `json:"question_id"`
	Type       ResponseType `json:"type"`
	Content    string       `json:"content,omitempty"`
}
