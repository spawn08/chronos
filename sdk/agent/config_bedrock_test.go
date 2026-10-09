package agent

import (
	"testing"

	"github.com/spawn08/chronos/engine/model"
)

func TestBuildProvider_Bedrock(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	p, err := buildProvider(ModelConfig{
		Provider: "bedrock",
		Model:    "anthropic.claude-opus-5-5",
		APIKey:   "bedrock-api-key",
		Region:   "eu-west-1",
	})
	if err != nil {
		t.Fatalf("buildProvider(bedrock): %v", err)
	}
	b, ok := p.(*model.Bedrock)
	if !ok {
		t.Fatalf("provider type %T, want *model.Bedrock", p)
	}
	if b.Name() != "bedrock" {
		t.Errorf("Name=%q", b.Name())
	}
	if b.Model() != "anthropic.claude-opus-5-5" {
		t.Errorf("Model=%q", b.Model())
	}
}

func TestBuildProvider_BedrockDefaults(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_REGION", "us-west-2")

	p, err := buildProvider(ModelConfig{Provider: "Bedrock"})
	if err != nil {
		t.Fatalf("buildProvider(bedrock): %v", err)
	}
	if _, ok := p.(*model.Bedrock); !ok {
		t.Fatalf("provider type %T, want *model.Bedrock", p)
	}
	if p.Model() != "anthropic.claude-sonnet-5-5" {
		t.Errorf("Model=%q, want the Bedrock default", p.Model())
	}
}

func TestExpandModelEnv_Region(t *testing.T) {
	t.Setenv("TEST_BEDROCK_REGION", "ap-southeast-2")
	m := ModelConfig{Region: "${TEST_BEDROCK_REGION}"}
	expandModelEnv(&m)
	if m.Region != "ap-southeast-2" {
		t.Errorf("Region=%q", m.Region)
	}
}
