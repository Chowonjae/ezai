package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go"
	openaiOption "github.com/openai/openai-go/option"

	"github.com/Chowonjae/ezai/internal/model"
)

// OllamaProvider - Ollama (로컬 LLM) 프로바이더 어댑터
// OpenAI 호환 API를 사용하므로 OpenAI Go SDK에 base_url만 변경한다.
type OllamaProvider struct {
	client  *openai.Client
	baseURL string
}

// NewOllamaProvider - Ollama 프로바이더 생성
func NewOllamaProvider(baseURL string) (*OllamaProvider, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1/"
	}
	// Ollama는 API 키가 불필요하지만 OpenAI SDK가 키를 요구하므로 더미 값 전달
	client := openai.NewClient(
		openaiOption.WithAPIKey("ollama"),
		openaiOption.WithBaseURL(baseURL),
	)
	return &OllamaProvider{client: &client, baseURL: baseURL}, nil
}

func (o *OllamaProvider) Name() string {
	return "ollama"
}

// Chat - Ollama 동기 요청
func (o *OllamaProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
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

	resp, err := o.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("Ollama API 호출 실패: %w", err)
	}

	responseText := ""
	if len(resp.Choices) > 0 {
		responseText = resp.Choices[0].Message.Content
	}

	// Ollama는 토큰 사용량을 반환하지 않을 수 있음
	return &model.ChatResponse{
		Provider: "ollama",
		Model:    resp.Model,
		Content:  responseText,
		Usage: model.UsageInfo{
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:  int(resp.Usage.TotalTokens),
		},
	}, nil
}

// ChatStream - Ollama 스트리밍 응답
func (o *OllamaProvider) ChatStream(ctx context.Context, req *model.ChatRequest) (<-chan model.StreamChunk, error) {
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

	stream := o.client.Chat.Completions.NewStreaming(ctx, params)

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

// Models - Ollama에 설치된 모델 목록을 실시간 조회
func (o *OllamaProvider) Models() []model.ModelInfo {
	// Ollama API: GET /api/tags
	apiURL := strings.TrimSuffix(o.baseURL, "/v1/")
	apiURL = strings.TrimSuffix(apiURL, "/v1")
	apiURL += "/api/tags"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	models := make([]model.ModelInfo, len(result.Models))
	for i, m := range result.Models {
		models[i] = model.ModelInfo{
			Provider:    "ollama",
			Model:       m.Name,
			DisplayName: m.Name,
			Available:   true,
		}
	}
	return models
}

// HealthCheck - Ollama 서버 상태 확인
func (o *OllamaProvider) HealthCheck(ctx context.Context) error {
	apiURL := strings.TrimSuffix(o.baseURL, "/v1/")
	apiURL = strings.TrimSuffix(apiURL, "/v1")

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Ollama 서버 연결 실패: %w", err)
	}
	resp.Body.Close()
	return nil
}
