package provider

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	openaiOption "github.com/openai/openai-go/option"

	"github.com/Chowonjae/ezai/internal/model"
)

// PerplexityProvider - Perplexity 프로바이더 어댑터
// OpenAI 호환 API를 사용하므로 OpenAI Go SDK에 base_url만 변경한다.
type PerplexityProvider struct {
	client *openai.Client
}

// NewPerplexityProvider - Perplexity 프로바이더 생성
func NewPerplexityProvider(apiKey string, baseURL string) (*PerplexityProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Perplexity API 키가 설정되지 않았습니다")
	}
	if baseURL == "" {
		baseURL = "https://api.perplexity.ai/"
	}
	client := openai.NewClient(
		openaiOption.WithAPIKey(apiKey),
		openaiOption.WithBaseURL(baseURL),
	)
	return &PerplexityProvider{client: &client}, nil
}

func (p *PerplexityProvider) Name() string {
	return "perplexity"
}

// Chat - Perplexity 동기 요청
func (p *PerplexityProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	messages := convertToOpenAIMessages(req.Messages)

	params := openai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: messages,
	}

	if req.Options.Temperature != nil {
		params.Temperature = openai.Float(*req.Options.Temperature)
	}
	if req.Options.MaxTokens != nil {
		params.MaxCompletionTokens = openai.Int(int64(*req.Options.MaxTokens))
	}
	if req.Options.TopP != nil {
		params.TopP = openai.Float(*req.Options.TopP)
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("Perplexity API 호출 실패: %w", err)
	}

	responseText := ""
	if len(resp.Choices) > 0 {
		responseText = resp.Choices[0].Message.Content
	}

	return &model.ChatResponse{
		Provider: "perplexity",
		Model:    resp.Model,
		Content:  responseText,
		Usage: model.UsageInfo{
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:  int(resp.Usage.TotalTokens),
		},
	}, nil
}

// ChatStream - Perplexity 스트리밍 응답
func (p *PerplexityProvider) ChatStream(ctx context.Context, req *model.ChatRequest) (<-chan model.StreamChunk, error) {
	messages := convertToOpenAIMessages(req.Messages)

	params := openai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: messages,
	}
	if req.Options.Temperature != nil {
		params.Temperature = openai.Float(*req.Options.Temperature)
	}
	if req.Options.MaxTokens != nil {
		params.MaxCompletionTokens = openai.Int(int64(*req.Options.MaxTokens))
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan model.StreamChunk, 32)

	go func() {
		defer close(ch)
		defer stream.Close()
		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				ch <- model.StreamChunk{Content: chunk.Choices[0].Delta.Content}
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

// Models - 사용 가능한 Perplexity 모델 목록
func (p *PerplexityProvider) Models() []model.ModelInfo {
	models := []string{
		"sonar-pro",
		"sonar",
		"sonar-reasoning-pro",
		"sonar-reasoning",
	}
	result := make([]model.ModelInfo, len(models))
	for i, m := range models {
		result[i] = model.ModelInfo{
			Provider:    "perplexity",
			Model:       m,
			DisplayName: m,
			Available:   true,
		}
	}
	return result
}

// HealthCheck - Perplexity 프로바이더 상태 확인
func (p *PerplexityProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: "sonar",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("hi"),
		},
		MaxCompletionTokens: openai.Int(1),
	})
	return err
}
