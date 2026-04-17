package provider

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicOption "github.com/anthropics/anthropic-sdk-go/option"

	"github.com/Chowonjae/ezai/internal/model"
)

// ClaudeProvider - Claude (Anthropic) 프로바이더 어댑터
type ClaudeProvider struct {
	client *anthropic.Client
}

// NewClaudeProvider - Claude 프로바이더 생성
func NewClaudeProvider(apiKey string) (*ClaudeProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Claude API 키가 설정되지 않았습니다")
	}
	client := anthropic.NewClient(anthropicOption.WithAPIKey(apiKey))
	return &ClaudeProvider{client: &client}, nil
}

func (c *ClaudeProvider) Name() string {
	return "claude"
}

// Chat - Claude 동기 요청
func (c *ClaudeProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	// 메시지 변환: system은 별도 필드로 분리
	messages, systemBlocks := convertToClaudeMessages(req.Messages)

	// 파라미터 구성
	maxTokens := int64(4096)
	if req.Options.MaxTokens != nil {
		maxTokens = int64(*req.Options.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: maxTokens,
		Messages:  messages,
	}

	// system 메시지 설정
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	// temperature 설정
	if req.Options.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Options.Temperature)
	}

	// top_p 설정
	if req.Options.TopP != nil {
		params.TopP = anthropic.Float(*req.Options.TopP)
	}

	// API 호출
	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("Claude API 호출 실패: %w", err)
	}

	// 응답 텍스트 추출
	responseText := extractClaudeText(resp)

	return &model.ChatResponse{
		Provider: "claude",
		Model:    resp.Model,
		Content:  responseText,
		Usage: model.UsageInfo{
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
			TotalTokens:  int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
		},
	}, nil
}

// ChatStream - Claude 스트리밍 응답
func (c *ClaudeProvider) ChatStream(ctx context.Context, req *model.ChatRequest) (<-chan model.StreamChunk, error) {
	messages, systemBlocks := convertToClaudeMessages(req.Messages)

	maxTokens := int64(4096)
	if req.Options.MaxTokens != nil {
		maxTokens = int64(*req.Options.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: maxTokens,
		Messages:  messages,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	if req.Options.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Options.Temperature)
	}
	if req.Options.TopP != nil {
		params.TopP = anthropic.Float(*req.Options.TopP)
	}

	stream := c.client.Messages.NewStreaming(ctx, params)

	ch := make(chan model.StreamChunk, 32)

	go func() {
		defer close(ch)
		for stream.Next() {
			event := stream.Current()

			switch v := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				if v.Delta.Text != "" {
					ch <- model.StreamChunk{Content: v.Delta.Text}
				}
			case anthropic.MessageDeltaEvent:
				ch <- model.StreamChunk{
					Done: true,
					Usage: &model.UsageInfo{
						OutputTokens: int(v.Usage.OutputTokens),
					},
				}
				return
			}
		}
		if err := stream.Err(); err != nil {
			errMsg := err.Error()
			ch <- model.StreamChunk{Error: &errMsg, Done: true}
			return
		}
		ch <- model.StreamChunk{Done: true}
	}()

	return ch, nil
}

// Models - 사용 가능한 Claude 모델 목록
func (c *ClaudeProvider) Models() []model.ModelInfo {
	models := []string{
		"claude-opus-4-6",
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
	}
	result := make([]model.ModelInfo, len(models))
	for i, m := range models {
		result[i] = model.ModelInfo{
			Provider:    "claude",
			Model:       m,
			DisplayName: m,
			Available:   true,
		}
	}
	return result
}

// HealthCheck - Claude 프로바이더 상태 확인
func (c *ClaudeProvider) HealthCheck(ctx context.Context) error {
	_, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     "claude-haiku-4-5-20251001",
		MaxTokens: 1,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	})
	return err
}

// convertToClaudeMessages - 통합 메시지를 Claude SDK 형식으로 변환
// system 역할은 별도 TextBlockParam 슬라이스로 분리한다.
func convertToClaudeMessages(messages []model.Message) ([]anthropic.MessageParam, []anthropic.TextBlockParam) {
	var claudeMessages []anthropic.MessageParam
	var systemBlocks []anthropic.TextBlockParam

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
				Text: msg.Content,
			})
		case "user":
			claudeMessages = append(claudeMessages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(msg.Content),
			))
		case "assistant":
			claudeMessages = append(claudeMessages, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(msg.Content),
			))
		}
	}

	return claudeMessages, systemBlocks
}

// extractClaudeText - Claude 응답에서 텍스트 추출
func extractClaudeText(resp *anthropic.Message) string {
	if resp == nil || len(resp.Content) == 0 {
		return ""
	}
	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return text
}
