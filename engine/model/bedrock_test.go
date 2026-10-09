package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

const bedrockTestModel = "anthropic.claude-opus-5-5"

func bedrockSuccessBody() string {
	return `{
		"id":"msg-1",
		"type":"message",
		"role":"assistant",
		"content":[{"type":"text","text":"Hello from Bedrock"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":5,"output_tokens":10,"cache_read_input_tokens":2}
	}`
}

// bedrockRecorder is an httptest handler that records every request and
// replies with the next scripted status and body (the last one repeats).
type bedrockRecorder struct {
	mu       sync.Mutex
	reqs     []*http.Request
	bodies   [][]byte
	statuses []int
	replies  []string
	stream   bool
}

func (rec *bedrockRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rec.mu.Lock()
	n := len(rec.reqs)
	rec.reqs = append(rec.reqs, r)
	rec.bodies = append(rec.bodies, body)
	status, reply := rec.statuses[min(n, len(rec.statuses)-1)], rec.replies[min(n, len(rec.replies)-1)]
	rec.mu.Unlock()
	if rec.stream && status == http.StatusOK {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.WriteHeader(status)
	fmt.Fprint(w, reply)
}

func (rec *bedrockRecorder) captured(t *testing.T, i int) (*http.Request, []byte) {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if i >= len(rec.reqs) {
		t.Fatalf("request %d not received (got %d)", i, len(rec.reqs))
	}
	return rec.reqs[i], rec.bodies[i]
}

func (rec *bedrockRecorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.reqs)
}

func newBedrockServer(t *testing.T, rec *bedrockRecorder) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return srv
}

// verifyBedrockSigV4 re-signs the received request with creds at its own
// X-Amz-Date over the headers it declared as signed and the body it carried,
// and reports whether the Authorization header matches.
func verifyBedrockSigV4(r *http.Request, body []byte, creds aws.Credentials) error {
	auth := r.Header.Get("Authorization")
	_, signed, ok := strings.Cut(auth, "SignedHeaders=")
	if !ok {
		return fmt.Errorf("no SignedHeaders in %q", auth)
	}
	signed, _, _ = strings.Cut(signed, ",")
	signTime, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		return fmt.Errorf("parse X-Amz-Date: %w", err)
	}
	replay, err := http.NewRequest(http.MethodPost, "http://"+r.Host+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build replay: %w", err)
	}
	for _, h := range strings.Split(signed, ";") {
		if h == "host" || h == "content-length" {
			continue
		}
		for _, v := range r.Header.Values(h) {
			replay.Header.Add(h, v)
		}
	}
	sum := sha256.Sum256(body)
	if err := v4.NewSigner().SignHTTP(context.Background(), creds, replay, hex.EncodeToString(sum[:]), "bedrock-mantle", "us-east-1", signTime); err != nil {
		return fmt.Errorf("re-sign: %w", err)
	}
	if want := replay.Header.Get("Authorization"); want != auth {
		return fmt.Errorf("signature mismatch:\n got  %s\n want %s", auth, want)
	}
	return nil
}

func assertBedrockSigV4(t *testing.T, r *http.Request, body []byte, creds aws.Credentials) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential="+creds.AccessKeyID+"/") {
		t.Errorf("Authorization=%q, want SigV4 with access key %s", auth, creds.AccessKeyID)
	}
	if !strings.Contains(auth, "/us-east-1/bedrock-mantle/aws4_request") {
		t.Errorf("Authorization=%q, want scope us-east-1/bedrock-mantle", auth)
	}
	if r.Header.Get("X-Amz-Date") == "" {
		t.Error("X-Amz-Date not set")
	}
	if got := r.Header.Get("x-api-key"); got != "" {
		t.Errorf("x-api-key=%q sent with SigV4", got)
	}
	if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version=%q", got)
	}
	if err := verifyBedrockSigV4(r, body, creds); err != nil {
		t.Error(err)
	}
}

func clearBedrockEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"AWS_BEARER_TOKEN_BEDROCK", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		t.Setenv(k, "")
	}
}

func TestNewBedrock_Defaults(t *testing.T) {
	clearBedrockEnv(t)
	b := NewBedrock("eu-west-1", "AKID", "SECRET", "")
	if b.Name() != "bedrock" {
		t.Errorf("Name=%q", b.Name())
	}
	if b.Model() != bedrockDefaultModel {
		t.Errorf("Model=%q, want %q", b.Model(), bedrockDefaultModel)
	}
	if b.region != "eu-west-1" {
		t.Errorf("region=%q", b.region)
	}
	if want := "https://bedrock-mantle.eu-west-1.api.aws/anthropic"; b.config.BaseURL != want {
		t.Errorf("BaseURL=%q, want %q", b.config.BaseURL, want)
	}
	if b.sigv4 == nil {
		t.Error("static credentials should sign with SigV4")
	}
	if _, ok := b.http.headers["x-api-key"]; ok {
		t.Error("x-api-key header set for SigV4")
	}
	if b.http.headers["anthropic-version"] != "2023-06-01" {
		t.Errorf("anthropic-version=%q", b.http.headers["anthropic-version"])
	}
}

func TestNewBedrock_ModelID(t *testing.T) {
	clearBedrockEnv(t)
	b := NewBedrock("us-east-1", "AKID", "SECRET", bedrockTestModel)
	if b.Model() != bedrockTestModel {
		t.Errorf("Model=%q, want %q", b.Model(), bedrockTestModel)
	}
}

func TestNewBedrockWithConfig_RegionFallback(t *testing.T) {
	clearBedrockEnv(t)
	if b := NewBedrockWithConfig("", ProviderConfig{APIKey: "tok"}, ""); b.region != "us-east-1" {
		t.Errorf("default region=%q, want us-east-1", b.region)
	}
	t.Setenv("AWS_DEFAULT_REGION", "ap-south-1")
	if b := NewBedrockWithConfig("", ProviderConfig{APIKey: "tok"}, ""); b.region != "ap-south-1" {
		t.Errorf("region=%q, want AWS_DEFAULT_REGION", b.region)
	}
	t.Setenv("AWS_REGION", "us-west-2")
	b := NewBedrockWithConfig("", ProviderConfig{APIKey: "tok"}, "")
	if b.region != "us-west-2" {
		t.Errorf("region=%q, want AWS_REGION", b.region)
	}
	if want := "https://bedrock-mantle.us-west-2.api.aws/anthropic"; b.config.BaseURL != want {
		t.Errorf("BaseURL=%q, want %q", b.config.BaseURL, want)
	}
	if b := NewBedrockWithConfig("eu-central-1", ProviderConfig{APIKey: "tok"}, ""); b.region != "eu-central-1" {
		t.Errorf("explicit region=%q", b.region)
	}
}

func TestNewBedrockWithConfig_AuthSelection(t *testing.T) {
	clearBedrockEnv(t)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{APIKey: "cfg-token"}, "")
	if b.sigv4 != nil || b.http.headers["x-api-key"] != "cfg-token" {
		t.Errorf("config bearer: sigv4=%v x-api-key=%q", b.sigv4 != nil, b.http.headers["x-api-key"])
	}

	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "env-token")
	b = NewBedrockWithConfig("us-east-1", ProviderConfig{}, "")
	if b.sigv4 != nil || b.http.headers["x-api-key"] != "env-token" {
		t.Errorf("env bearer: sigv4=%v x-api-key=%q", b.sigv4 != nil, b.http.headers["x-api-key"])
	}
	b = NewBedrockWithConfig("us-east-1", ProviderConfig{APIKey: "cfg-token"}, "")
	if b.http.headers["x-api-key"] != "cfg-token" {
		t.Errorf("config token should win over env, got %q", b.http.headers["x-api-key"])
	}

	// A secret key means SigV4, even with a bearer token in the environment.
	b = NewBedrockWithConfig("us-east-1", ProviderConfig{APIKey: "AKID"}, "SECRET")
	if b.sigv4 == nil || b.config.APIKey != "" {
		t.Errorf("static SigV4: sigv4=%v APIKey=%q", b.sigv4 != nil, b.config.APIKey)
	}

	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	b = NewBedrockWithConfig("us-east-1", ProviderConfig{}, "")
	if b.sigv4 == nil {
		t.Error("no token should fall back to SigV4 with the default credential chain")
	}
}

func TestBedrock_Chat_BearerToolRound(t *testing.T) {
	clearBedrockEnv(t)
	reply := `{
		"id":"msg-2","type":"message","role":"assistant",
		"content":[{"type":"text","text":"Checking."},{"type":"tool_use","id":"toolu_2","name":"get_weather","input":{"city":"Lyon"}}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":20,"output_tokens":8,"cache_creation_input_tokens":4,"cache_read_input_tokens":16}
	}`
	rec := &bedrockRecorder{statuses: []int{http.StatusOK}, replies: []string{reply}}
	srv := newBedrockServer(t, rec)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{
		APIKey:  "bedrock-api-key",
		BaseURL: srv.URL + "/anthropic",
		Model:   bedrockTestModel,
	}, "")
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are helpful."},
			{Role: RoleSystem, Content: "Project context."},
			{Role: RoleUser, Content: "Weather in Paris?"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "toolu_1", Name: "get_weather", Arguments: `{"city":"Paris"}`}}},
			{Role: RoleTool, ToolCallID: "toolu_1", Content: "sunny"},
		},
		Tools: []ToolDefinition{{Type: "function", Function: FunctionDef{
			Name:        "get_weather",
			Description: "Get the weather",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
		}}},
	}
	resp, err := b.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	r, raw := rec.captured(t, 0)
	if r.URL.Path != "/anthropic/v1/messages" {
		t.Errorf("path=%q, want /anthropic/v1/messages", r.URL.Path)
	}
	if got := r.Header.Get("x-api-key"); got != "bedrock-api-key" {
		t.Errorf("x-api-key=%q", got)
	}
	if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version=%q", got)
	}
	if got := r.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization=%q, want none with a bearer token", got)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// The body must be exactly what the first-party Anthropic provider sends.
	wantRaw, err := json.Marshal(NewAnthropicWithConfig(ProviderConfig{Model: bedrockTestModel}).buildRequestBody(req, false))
	if err != nil {
		t.Fatalf("marshal anthropic body: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatalf("decode anthropic body: %v", err)
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body differs from Anthropic provider:\n got  %s\n want %s", raw, wantRaw)
	}

	if body["model"] != bedrockTestModel {
		t.Errorf("model=%v", body["model"])
	}
	system, _ := body["system"].([]any)
	if len(system) != 2 {
		t.Fatalf("system=%v, want two cached blocks", body["system"])
	}
	if first, _ := system[0].(map[string]any); first["cache_control"] == nil || first["text"] != "You are helpful." {
		t.Errorf("system[0]=%v, want text with cache_control", system[0])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages=%d, want 3", len(msgs))
	}
	assistant, _ := msgs[1].(map[string]any)
	blocks, _ := assistant["content"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("assistant content=%v", assistant["content"])
	}
	toolUse, _ := blocks[0].(map[string]any)
	input, _ := toolUse["input"].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["id"] != "toolu_1" || toolUse["name"] != "get_weather" || input["city"] != "Paris" {
		t.Errorf("tool_use block=%v", toolUse)
	}
	result, _ := msgs[2].(map[string]any)
	resultBlocks, _ := result["content"].([]any)
	if result["role"] != RoleUser || len(resultBlocks) != 1 {
		t.Fatalf("tool result message=%v", result)
	}
	toolResult, _ := resultBlocks[0].(map[string]any)
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "toolu_1" || toolResult["content"] != "sunny" {
		t.Errorf("tool_result block=%v", toolResult)
	}
	if toolResult["cache_control"] == nil {
		t.Error("last message should carry the tail cache breakpoint")
	}
	if tools, _ := body["tools"].([]any); len(tools) != 1 {
		t.Errorf("tools=%v", body["tools"])
	}

	if resp.Content != "Checking." || resp.StopReason != StopReasonToolCall {
		t.Errorf("Content=%q StopReason=%q", resp.Content, resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "toolu_2" || resp.ToolCalls[0].Arguments != `{"city":"Lyon"}` {
		t.Errorf("ToolCalls=%+v", resp.ToolCalls)
	}
	if !resp.UsageKnown || resp.Usage.PromptTokens != 20 || resp.Usage.CompletionTokens != 8 ||
		resp.Usage.CacheCreationTokens != 4 || resp.Usage.CacheReadTokens != 16 {
		t.Errorf("Usage=%+v known=%v", resp.Usage, resp.UsageKnown)
	}
}

func TestBedrock_Chat_EnvBearerToken(t *testing.T) {
	clearBedrockEnv(t)
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "env-token")
	rec := &bedrockRecorder{statuses: []int{http.StatusOK}, replies: []string{bedrockSuccessBody()}}
	srv := newBedrockServer(t, rec)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{BaseURL: srv.URL + "/anthropic"}, "")
	resp, err := b.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from Bedrock" {
		t.Errorf("Content=%q", resp.Content)
	}
	r, raw := rec.captured(t, 0)
	if got := r.Header.Get("x-api-key"); got != "env-token" {
		t.Errorf("x-api-key=%q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["model"] != bedrockDefaultModel {
		t.Errorf("model=%v, want default %s", body["model"], bedrockDefaultModel)
	}
}

func TestBedrock_Chat_SigV4StaticCredentials(t *testing.T) {
	clearBedrockEnv(t)
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "ignored-with-secret-key")
	rec := &bedrockRecorder{statuses: []int{http.StatusOK}, replies: []string{bedrockSuccessBody()}}
	srv := newBedrockServer(t, rec)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{
		APIKey:  "AKID",
		BaseURL: srv.URL + "/anthropic",
		Model:   bedrockTestModel,
	}, "SECRET")
	if _, err := b.Chat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	r, body := rec.captured(t, 0)
	if r.URL.Path != "/anthropic/v1/messages" {
		t.Errorf("path=%q", r.URL.Path)
	}
	assertBedrockSigV4(t, r, body, aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET"})
	if got := r.Header.Get("X-Amz-Security-Token"); got != "" {
		t.Errorf("X-Amz-Security-Token=%q without a session token", got)
	}
}

func TestBedrock_Chat_SigV4SessionToken(t *testing.T) {
	clearBedrockEnv(t)
	rec := &bedrockRecorder{statuses: []int{http.StatusOK}, replies: []string{bedrockSuccessBody()}}
	srv := newBedrockServer(t, rec)

	b := newBedrock("us-east-1", ProviderConfig{BaseURL: srv.URL + "/anthropic", Model: bedrockTestModel},
		credentials.NewStaticCredentialsProvider("ASIATEMP", "SECRET", "SESSION"))
	if _, err := b.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	r, body := rec.captured(t, 0)
	if got := r.Header.Get("X-Amz-Security-Token"); got != "SESSION" {
		t.Errorf("X-Amz-Security-Token=%q, want SESSION", got)
	}
	assertBedrockSigV4(t, r, body, aws.Credentials{AccessKeyID: "ASIATEMP", SecretAccessKey: "SECRET", SessionToken: "SESSION"})
}

func TestBedrock_Chat_SigV4DefaultChainFromEnv(t *testing.T) {
	clearBedrockEnv(t)
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDENV")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "SECRETENV")
	t.Setenv("AWS_SESSION_TOKEN", "TOKENENV")
	rec := &bedrockRecorder{statuses: []int{http.StatusOK}, replies: []string{bedrockSuccessBody()}}
	srv := newBedrockServer(t, rec)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{BaseURL: srv.URL + "/anthropic"}, "")
	if b.sigv4 == nil {
		t.Fatal("expected SigV4 with the default credential chain")
	}
	if _, err := b.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	r, body := rec.captured(t, 0)
	assertBedrockSigV4(t, r, body, aws.Credentials{AccessKeyID: "AKIDENV", SecretAccessKey: "SECRETENV", SessionToken: "TOKENENV"})
}

func TestBedrock_Chat_RetryResigns(t *testing.T) {
	clearBedrockEnv(t)
	rec := &bedrockRecorder{
		statuses: []int{http.StatusInternalServerError, http.StatusOK},
		replies:  []string{`{"type":"error","error":{"type":"api_error","message":"boom"}}`, bedrockSuccessBody()},
	}
	srv := newBedrockServer(t, rec)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{
		APIKey:     "AKID",
		BaseURL:    srv.URL + "/anthropic",
		Model:      bedrockTestModel,
		MaxRetries: 1,
	}, "SECRET")
	b.http.sleep = func(context.Context, time.Duration) error { return nil }
	// Each signing reads the clock; advance it so the retry's timestamp must
	// differ from the first attempt's.
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	b.sigv4.now = func() time.Time {
		clock = clock.Add(2 * time.Second)
		return clock
	}

	resp, err := b.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from Bedrock" {
		t.Errorf("Content=%q", resp.Content)
	}
	if n := rec.count(); n != 2 {
		t.Fatalf("requests=%d, want 2", n)
	}
	creds := aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET"}
	first, firstBody := rec.captured(t, 0)
	second, secondBody := rec.captured(t, 1)
	assertBedrockSigV4(t, first, firstBody, creds)
	assertBedrockSigV4(t, second, secondBody, creds)
	if !bytes.Equal(firstBody, secondBody) {
		t.Error("retry body differs from the first attempt")
	}
	if first.Header.Get("X-Amz-Date") != "20261009T120002Z" || second.Header.Get("X-Amz-Date") != "20261009T120004Z" {
		t.Errorf("X-Amz-Date first=%q second=%q, want a fresh timestamp per attempt",
			first.Header.Get("X-Amz-Date"), second.Header.Get("X-Amz-Date"))
	}
	if first.Header.Get("Authorization") == second.Header.Get("Authorization") {
		t.Error("retry reused the first attempt's signature")
	}
}

func TestBedrock_Chat_SignFailureNotRetried(t *testing.T) {
	clearBedrockEnv(t)
	rec := &bedrockRecorder{statuses: []int{http.StatusOK}, replies: []string{bedrockSuccessBody()}}
	srv := newBedrockServer(t, rec)

	credErr := errors.New("no credentials")
	b := newBedrock("us-east-1", ProviderConfig{BaseURL: srv.URL + "/anthropic", MaxRetries: 3},
		aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{}, credErr
		}))
	b.http.sleep = func(context.Context, time.Duration) error {
		t.Error("signing failure should not back off and retry")
		return nil
	}
	_, err := b.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if !errors.Is(err, credErr) {
		t.Fatalf("err=%v, want the credential error", err)
	}
	if n := rec.count(); n != 0 {
		t.Errorf("requests=%d, want none sent unsigned", n)
	}
}

func TestBedrock_Chat_Error(t *testing.T) {
	clearBedrockEnv(t)
	rec := &bedrockRecorder{
		statuses: []int{http.StatusUnauthorized},
		replies:  []string{`{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`},
	}
	srv := newBedrockServer(t, rec)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{APIKey: "tok", BaseURL: srv.URL + "/anthropic"}, "")
	_, err := b.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("err=%v, want wrapped *APIError 401", err)
	}
	if !strings.HasPrefix(err.Error(), "bedrock: ") {
		t.Errorf("err=%q, want bedrock prefix", err)
	}
}

func TestBedrock_StreamChat_SSE(t *testing.T) {
	clearBedrockEnv(t)
	events := []string{
		`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg-3","type":"message","role":"assistant","content":[],"usage":{"input_tokens":12,"output_tokens":1,"cache_read_input_tokens":3}}}`,
		`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}`,
		`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
		`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
	}
	rec := &bedrockRecorder{
		statuses: []int{http.StatusOK},
		replies:  []string{strings.Join(events, "\n\n") + "\n\n"},
		stream:   true,
	}
	srv := newBedrockServer(t, rec)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{
		APIKey:  "AKID",
		BaseURL: srv.URL + "/anthropic",
		Model:   bedrockTestModel,
	}, "SECRET")
	ch, err := b.StreamChat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var content strings.Builder
	var prompt, completion, cacheRead int
	var stop StopReason
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		content.WriteString(chunk.Content)
		if chunk.UsageKnown {
			prompt = max(prompt, chunk.Usage.PromptTokens)
			completion = max(completion, chunk.Usage.CompletionTokens)
			cacheRead = max(cacheRead, chunk.Usage.CacheReadTokens)
		}
		if chunk.StopReason != "" {
			stop = chunk.StopReason
		}
	}
	if content.String() != "Hello there" {
		t.Errorf("content=%q", content.String())
	}
	if prompt != 12 || completion != 7 || cacheRead != 3 {
		t.Errorf("usage prompt=%d completion=%d cacheRead=%d", prompt, completion, cacheRead)
	}
	if stop != StopReasonEnd {
		t.Errorf("StopReason=%q", stop)
	}

	r, raw := rec.captured(t, 0)
	if r.URL.Path != "/anthropic/v1/messages" {
		t.Errorf("path=%q", r.URL.Path)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["stream"] != true {
		t.Errorf("stream=%v, want true", body["stream"])
	}
	assertBedrockSigV4(t, r, raw, aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET"})
}

func TestBedrock_StreamChat_HTTPError(t *testing.T) {
	clearBedrockEnv(t)
	rec := &bedrockRecorder{statuses: []int{http.StatusBadRequest}, replies: []string{`{"type":"error"}`}}
	srv := newBedrockServer(t, rec)

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{APIKey: "tok", BaseURL: srv.URL + "/anthropic"}, "")
	_, err := b.StreamChat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
	if !strings.HasPrefix(err.Error(), "bedrock: ") {
		t.Errorf("err=%q, want bedrock prefix", err)
	}
}
