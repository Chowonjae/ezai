## 프로젝트                                                                                                     
- ezai: 멀티 AI Gateway 서비스                                                                                
- 설계 문서: /Users/violet/llm/ezai/EZAI_GATEWAY_SPEC.md (반드시 읽고 작업할 것)

## 언어 및 기술                                                                                                 
- Go로 개발 (Python 아님)                                                                                       
- HTTP 프레임워크: net/http 또는 Gin/Echo                                                                       
- DB: SQLite + SQLCipher (외부 DB 사용 금지, 확장 시에만 PostgreSQL)                                            
- 설정 파일: YAML + Git                                                                                         
                                                                                                                  
## 프로바이더 SDK (반드시 준수)                                                                                 
- Gemini: Vertex AI Go SDK (google.golang.org/genai)                                                            
- Claude: Anthropic Go SDK                                                                                      
- GPT: OpenAI Go SDK
- Perplexity: OpenAI Go SDK (base_url 변경)                                                                               
- Ollama: OpenAI Go SDK (base_url 변경)                                                                         
- 각 프로바이더는 반드시 공식 SDK를 사용할 것                                                                   
                                                                                                                
## 데이터 저장소 규칙                                                                                           
- API 키, 시크릿 → SQLite (암호화) 절대 YAML/파일에 저장 금지                                                   
- 프롬프트, 설정, 가격 테이블 → YAML                                                                            
- 로그/비용 → SQLite (request_logs 테이블 하나로 통합)

## 개발 유의사항
1. 모든 소스 반영시에는 실행 계획을 세워 사용자의 컨펌을 받아야 합니다.
2. 주요 소스에 주석을 함께 작성해야 합니다. 
3. 소스 구현에 수정사항이 발생 할 경우 해당 수정으로 인해 기존 로직이 문제가 발생하지 않도록 한다.
4. struct에 필드를 추가하거나 함수 시그니처를 변경할 때, 컴파일 에러 수정만으로 끝내지 말고 해당 struct/함수를 사용하는 모든 곳을 grep으로 확인하여 누락이 없는지 검증한다.
