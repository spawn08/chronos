// Package providers is a shared helper used by the runnable examples to pick
// an LLM provider from the environment.
//
// The precedence order — OpenAI → Anthropic → Gemini → Azure OpenAI → Vertex
// AI → AWS Bedrock → Mistral → Ollama → OpenAI-compatible — is arbitrary; it
// simply lets an example script run with whatever credentials the user
// happens to have exported. Every example wired through Pick automatically
// gains support for the full provider matrix.
package providers

import (
	"fmt"
	"os"

	"github.com/spawn08/chronos/engine/model"
)

// Pick returns the first configured provider from the environment along with
// its human-readable name, or (nil, "") if nothing is configured.
//
// Recognized env vars (checked in order):
//
//	OPENAI_API_KEY                              → OpenAI
//	ANTHROPIC_API_KEY                           → Anthropic
//	GEMINI_API_KEY                              → Google Gemini (AI Studio)
//	AZURE_OPENAI_API_KEY + AZURE_OPENAI_ENDPOINT
//	  + AZURE_OPENAI_DEPLOYMENT                 → Azure OpenAI
//	GOOGLE_CLOUD_PROJECT + GOOGLE_ACCESS_TOKEN  → Google Cloud Vertex AI
//	AWS_REGION + AWS_ACCESS_KEY_ID
//	  + AWS_SECRET_ACCESS_KEY                   → AWS Bedrock
//	MISTRAL_API_KEY                             → Mistral
//	OLLAMA_HOST                                 → Ollama (local)
//	OPENAI_COMPATIBLE_BASE_URL
//	  + OPENAI_COMPATIBLE_MODEL                 → any OpenAI-compatible endpoint
func Pick() (model.Provider, string) {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return model.NewOpenAI(key), "OpenAI"
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return model.NewAnthropic(key), "Anthropic"
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		return model.NewGemini(key), "Gemini"
	}
	if p, ok := azure(); ok {
		return p, "AzureOpenAI"
	}
	if p, ok := vertex(); ok {
		return p, "Vertex"
	}
	if p, ok := bedrock(); ok {
		return p, "Bedrock"
	}
	if key := os.Getenv("MISTRAL_API_KEY"); key != "" {
		return model.NewMistral(key), "Mistral"
	}
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		return model.NewOllama(host, envOr("OLLAMA_MODEL", "llama3.2")), "Ollama"
	}
	if base := os.Getenv("OPENAI_COMPATIBLE_BASE_URL"); base != "" {
		return model.NewOpenAICompatible(
			envOr("OPENAI_COMPATIBLE_NAME", "compatible"),
			base,
			os.Getenv("OPENAI_COMPATIBLE_API_KEY"),
			envOr("OPENAI_COMPATIBLE_MODEL", "default"),
		), "Compatible"
	}
	return nil, ""
}

// EnvHint returns the multi-line message to print when no provider is
// configured. Consistent across examples so users see the same option set.
func EnvHint() string {
	return "Set one of the following to run with a real LLM:\n" +
		"  OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY, MISTRAL_API_KEY\n" +
		"  AZURE_OPENAI_API_KEY + AZURE_OPENAI_ENDPOINT + AZURE_OPENAI_DEPLOYMENT\n" +
		"  GOOGLE_CLOUD_PROJECT + GOOGLE_ACCESS_TOKEN (Vertex AI)\n" +
		"  AWS_REGION + AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY (Bedrock)\n" +
		"  OLLAMA_HOST (local)\n" +
		"  OPENAI_COMPATIBLE_BASE_URL + OPENAI_COMPATIBLE_MODEL (Together, Groq, vLLM, LiteLLM…)"
}

func azure() (model.Provider, bool) {
	key := os.Getenv("AZURE_OPENAI_API_KEY")
	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if key == "" || endpoint == "" || deployment == "" {
		return nil, false
	}
	return model.NewAzureOpenAIWithConfig(model.AzureConfig{
		ProviderConfig: model.ProviderConfig{APIKey: key, BaseURL: endpoint},
		Deployment:     deployment,
		APIVersion:     envOr("AZURE_OPENAI_API_VERSION", "2024-12-01-preview"),
	}), true
}

func vertex() (model.Provider, bool) {
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	token := os.Getenv("GOOGLE_ACCESS_TOKEN")
	if project == "" || token == "" {
		return nil, false
	}
	location := envOr("GOOGLE_CLOUD_LOCATION", "us-central1")
	base := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/endpoints/openapi",
		location, project, location,
	)
	return model.NewOpenAICompatibleWithConfig("vertex", model.ProviderConfig{
		APIKey:  token,
		BaseURL: base,
		Model:   envOr("VERTEX_MODEL", "google/gemini-2.5-pro"),
	}), true
}

func bedrock() (model.Provider, bool) {
	region := os.Getenv("AWS_REGION")
	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if region == "" || ak == "" || sk == "" {
		return nil, false
	}
	return model.NewBedrock(region, ak, sk,
		envOr("BEDROCK_MODEL_ID", "anthropic.claude-3-5-sonnet-20241022-v2:0")), true
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
