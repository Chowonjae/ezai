package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/auth/credentials"
	"google.golang.org/genai"

	"github.com/Chowonjae/ezai/internal/model"
)

// GeminiProvider - Gemini (Vertex AI) 프로바이더 어댑터
type GeminiProvider struct {
	client    *genai.Client
	projectID string
	location  string
}

// GeminiConfig - Gemini 프로바이더 초기화 설정
type GeminiConfig struct {
	ProjectID          string // GCP 프로젝트 ID (빈 문자열이면 JSON에서 추출)
	Location           string // Vertex AI 리전 (예: us-central1)
	ServiceAccountJSON string // 서비스 계정 JSON 내용 (keys.db에서 로드)
}

// NewGeminiProvider - Gemini 프로바이더 생성
// 인증 우선순위: ServiceAccountJSON(keys.db) → GOOGLE_APPLICATION_CREDENTIALS(환경변수) → ADC
func NewGeminiProvider(ctx context.Context, cfg GeminiConfig) (*GeminiProvider, error) {
	// 서비스 계정 JSON에서 프로젝트 ID 자동 추출
	projectID := cfg.ProjectID
	if projectID == "" && cfg.ServiceAccountJSON != "" {
		var sa struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal([]byte(cfg.ServiceAccountJSON), &sa); err == nil && sa.ProjectID != "" {
			projectID = sa.ProjectID
		}
	}

	clientCfg := &genai.ClientConfig{
		Project:  projectID,
		Location: cfg.Location,
		Backend:  genai.BackendVertexAI,
	}

	// 서비스 계정 JSON이 있으면 직접 인증 (파일 불필요)
	if cfg.ServiceAccountJSON != "" {
		creds, err := credentials.DetectDefault(&credentials.DetectOptions{
			CredentialsJSON: []byte(cfg.ServiceAccountJSON),
			Scopes:          []string{"https://www.googleapis.com/auth/cloud-platform"},
		})
		if err != nil {
			return nil, fmt.Errorf("서비스 계정 인증 실패: %w", err)
		}
		clientCfg.Credentials = creds
	}

	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("Gemini 클라이언트 생성 실패: %w", err)
	}

	return &GeminiProvider{
		client:    client,
		projectID: projectID,
		location:  cfg.Location,
	}, nil
}

func (g *GeminiProvider) Name() string {
	return "gemini"
}

// Chat - Gemini 동기 요청
func (g *GeminiProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	// 메시지를 Gemini SDK 형식으로 변환
	contents, systemInstruction := convertToGeminiMessages(req.Messages)

	// 생성 설정 구성
	genConfig := &genai.GenerateContentConfig{}
	if req.Options.Temperature != nil {
		temp := float32(*req.Options.Temperature)
		genConfig.Temperature = &temp
	}
	if req.Options.MaxTokens != nil {
		genConfig.MaxOutputTokens = int32(*req.Options.MaxTokens)
	}
	if req.Options.TopP != nil {
		topP := float32(*req.Options.TopP)
		genConfig.TopP = &topP
	}
	if systemInstruction != "" {
		genConfig.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(systemInstruction)},
		}
	}

	// Search Grounding 설정
	if req.Options.SearchGrounding {
		genConfig.Tools = []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		}
	}

	// API 호출
	resp, err := g.client.Models.GenerateContent(ctx, req.Model, contents, genConfig)
	if err != nil {
		return nil, fmt.Errorf("Gemini API 호출 실패: %w", err)
	}

	// 응답 텍스트 추출
	responseText := extractGeminiText(resp)

	// 토큰 사용량 추출
	usage := model.UsageInfo{}
	if resp.UsageMetadata != nil {
		usage.InputTokens = int(resp.UsageMetadata.PromptTokenCount)
		usage.OutputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
		usage.TotalTokens = int(resp.UsageMetadata.TotalTokenCount)
	}

	// Search Grounding 소스 추출
	var searchSources []model.SearchSource
	if req.Options.SearchGrounding {
		searchSources = extractGroundingSources(resp)
	}

	return &model.ChatResponse{
		Provider: "gemini",
		Model:    req.Model,
		Content:  responseText,
		Usage:    usage,
		Metadata: model.ResponseMetadata{
			SearchSources: searchSources,
		},
	}, nil
}

// ChatStream - Gemini 스트리밍 응답
func (g *GeminiProvider) ChatStream(ctx context.Context, req *model.ChatRequest) (<-chan model.StreamChunk, error) {
	contents, systemInstruction := convertToGeminiMessages(req.Messages)

	genConfig := &genai.GenerateContentConfig{}
	if req.Options.Temperature != nil {
		temp := float32(*req.Options.Temperature)
		genConfig.Temperature = &temp
	}
	if req.Options.MaxTokens != nil {
		genConfig.MaxOutputTokens = int32(*req.Options.MaxTokens)
	}
	if req.Options.TopP != nil {
		topP := float32(*req.Options.TopP)
		genConfig.TopP = &topP
	}
	if systemInstruction != "" {
		genConfig.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(systemInstruction)},
		}
	}
	if req.Options.SearchGrounding {
		genConfig.Tools = []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		}
	}

	ch := make(chan model.StreamChunk, 32)

	go func() {
		defer close(ch)
		for result, err := range g.client.Models.GenerateContentStream(ctx, req.Model, contents, genConfig) {
			if err != nil {
				errMsg := err.Error()
				ch <- model.StreamChunk{Error: &errMsg, Done: true}
				return
			}
			text := extractGeminiText(result)
			if text != "" {
				ch <- model.StreamChunk{Content: text}
			}
			// 마지막 청크에 사용량 정보 포함
			if result.UsageMetadata != nil && result.UsageMetadata.CandidatesTokenCount > 0 {
				ch <- model.StreamChunk{
					Done: true,
					Usage: &model.UsageInfo{
						InputTokens:  int(result.UsageMetadata.PromptTokenCount),
						OutputTokens: int(result.UsageMetadata.CandidatesTokenCount),
						TotalTokens:  int(result.UsageMetadata.TotalTokenCount),
					},
				}
				return
			}
		}
		// 이터레이터 정상 종료
		ch <- model.StreamChunk{Done: true}
	}()

	return ch, nil
}

// Models - 사용 가능한 Gemini 모델 목록
func (g *GeminiProvider) Models() []model.ModelInfo {
	models := []string{
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-2.0-flash",
		"gemini-2.0-flash-lite",
	}
	result := make([]model.ModelInfo, len(models))
	for i, m := range models {
		result[i] = model.ModelInfo{
			Provider:    "gemini",
			Model:       m,
			DisplayName: m,
			Available:   true,
		}
	}
	return result
}

// HealthCheck - Gemini 프로바이더 상태 확인
func (g *GeminiProvider) HealthCheck(ctx context.Context) error {
	// 간단한 요청으로 연결 확인
	_, err := g.client.Models.GenerateContent(ctx, "gemini-2.5-flash", []*genai.Content{
		genai.NewContentFromText("hi", "user"),
	}, &genai.GenerateContentConfig{
		MaxOutputTokens: 1,
	})
	return err
}

// convertToGeminiMessages - 통합 메시지를 Gemini SDK 형식으로 변환
// system 역할은 별도 systemInstruction으로 분리한다.
func convertToGeminiMessages(messages []model.Message) ([]*genai.Content, string) {
	var contents []*genai.Content
	var systemParts []string

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			systemParts = append(systemParts, msg.Content)
		case "user":
			contents = append(contents, genai.NewContentFromText(msg.Content, "user"))
		case "assistant":
			contents = append(contents, genai.NewContentFromText(msg.Content, "model"))
		}
	}

	systemInstruction := strings.Join(systemParts, "\n")
	return contents, systemInstruction
}

// extractGeminiText - Gemini 응답에서 텍스트 추출
func extractGeminiText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}
	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return ""
	}
	var texts []string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "")
}

// extractGroundingSources - Gemini 응답에서 Search Grounding 소스 추출
func extractGroundingSources(resp *genai.GenerateContentResponse) []model.SearchSource {
	if resp == nil || len(resp.Candidates) == 0 {
		return nil
	}
	candidate := resp.Candidates[0]
	metadata := candidate.GroundingMetadata
	if metadata == nil || len(metadata.GroundingChunks) == 0 {
		return nil
	}

	var sources []model.SearchSource
	for _, chunk := range metadata.GroundingChunks {
		if chunk.Web != nil && chunk.Web.URI != "" {
			sources = append(sources, model.SearchSource{
				Title: chunk.Web.Title,
				URI:   chunk.Web.URI,
			})
		}
	}
	return sources
}

