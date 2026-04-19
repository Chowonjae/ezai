package provider

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	openaiOption "github.com/openai/openai-go/option"

	"github.com/Chowonjae/ezai/internal/model"
)

// GPTProvider - GPT (OpenAI) 프로바이더 어댑터
type GPTProvider struct {
	client *openai.Client
}

// NewGPTProvider - GPT 프로바이더 생성
func NewGPTProvider(apiKey string) (*GPTProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API 키가 설정되지 않았습니다")
	}
	client := openai.NewClient(openaiOption.WithAPIKey(apiKey))
	return &GPTProvider{client: &client}, nil
}

func (g *GPTProvider) Name() string {
	return "gpt"
}

// Chat - GPT 동기 요청
func (g *GPTProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	// 메시지 변환
	messages := convertToOpenAIMessages(req.Messages)

	params := openai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: messages,
	}

	// 옵션 설정
	if req.Options.Temperature != nil {
		params.Temperature = openai.Float(*req.Options.Temperature)
	}
	if req.Options.MaxTokens != nil {
		params.MaxCompletionTokens = openai.Int(int64(*req.Options.MaxTokens))
	}
	if req.Options.TopP != nil {
		params.TopP = openai.Float(*req.Options.TopP)
	}

	// API 호출
	resp, err := g.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("GPT API 호출 실패: %w", err)
	}

	// 응답 텍스트 추출
	responseText := ""
	if len(resp.Choices) > 0 {
		responseText = resp.Choices[0].Message.Content
	}

	return &model.ChatResponse{
		Provider: "gpt",
		Model:    resp.Model,
		Content:  responseText,
		Usage: model.UsageInfo{
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:  int(resp.Usage.TotalTokens),
		},
	}, nil
}

// ChatStream - GPT 스트리밍 응답
func (g *GPTProvider) ChatStream(ctx context.Context, req *model.ChatRequest) (<-chan model.StreamChunk, error) {
	messages := convertToOpenAIMessages(req.Messages)

	params := openai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: messages,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}
	if req.Options.Temperature != nil {
		params.Temperature = openai.Float(*req.Options.Temperature)
	}
	if req.Options.MaxTokens != nil {
		params.MaxCompletionTokens = openai.Int(int64(*req.Options.MaxTokens))
	}

	stream := g.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan model.StreamChunk, 32)

	go func() {
		defer close(ch)
		defer stream.Close()
		for stream.Next() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			chunk := stream.Current()
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				select {
				case ch <- model.StreamChunk{Content: chunk.Choices[0].Delta.Content}:
				case <-ctx.Done():
					return
				}
			}
			if chunk.Usage.TotalTokens > 0 {
				select {
				case ch <- model.StreamChunk{
					Done: true,
					Usage: &model.UsageInfo{
						InputTokens:  int(chunk.Usage.PromptTokens),
						OutputTokens: int(chunk.Usage.CompletionTokens),
						TotalTokens:  int(chunk.Usage.TotalTokens),
					},
				}:
				case <-ctx.Done():
				}
				return
			}
		}
		if err := stream.Err(); err != nil {
			errMsg := err.Error()
			select {
			case ch <- model.StreamChunk{Error: &errMsg, Done: true}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case ch <- model.StreamChunk{Done: true}:
		case <-ctx.Done():
		}
	}()

	return ch, nil
}

// Models - 사용 가능한 GPT 모델 목록
func (g *GPTProvider) Models() []model.ModelInfo {
	models := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4-turbo",
	}
	result := make([]model.ModelInfo, len(models))
	for i, m := range models {
		result[i] = model.ModelInfo{
			Provider:    "gpt",
			Model:       m,
			DisplayName: m,
			Available:   true,
		}
	}
	return result
}

// HealthCheck - GPT 프로바이더 상태 확인
func (g *GPTProvider) HealthCheck(ctx context.Context) error {
	_, err := g.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("hi"),
		},
		MaxCompletionTokens: openai.Int(1),
	})
	return err
}

// convertToOpenAIMessages - 통합 메시지를 OpenAI SDK 형식으로 변환
func convertToOpenAIMessages(messages []model.Message) []openai.ChatCompletionMessageParamUnion {
	var result []openai.ChatCompletionMessageParamUnion
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			result = append(result, openai.SystemMessage(msg.Content))
		case "user":
			result = append(result, openai.UserMessage(msg.Content))
		case "assistant":
			result = append(result, openai.AssistantMessage(msg.Content))
		}
	}
	return result
}
