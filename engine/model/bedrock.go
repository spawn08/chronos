package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

const (
	bedrockDefaultModel  = "anthropic.claude-sonnet-5-5"
	bedrockDefaultRegion = "us-east-1"
	// bedrockSigningName is the SigV4 service name of the Mantle endpoint.
	bedrockSigningName = "bedrock-mantle"
)

// Bedrock implements Provider for Claude in Amazon Bedrock through the Bedrock
// Mantle Messages endpoint (https://bedrock-mantle.{region}.api.aws/anthropic).
// That endpoint takes the first-party Messages request body and streams the
// same SSE events, so Bedrock reuses the Anthropic provider for request
// building, prompt caching, thinking and stream parsing; only the endpoint and
// authentication differ. Model IDs carry an "anthropic." prefix, for example
// "anthropic.claude-sonnet-5-5".
type Bedrock struct {
	*Anthropic
	region string
	// sigv4 is the request signer; nil when a bearer token authenticates.
	sigv4 *bedrockSigV4
}

// NewBedrock creates a Bedrock provider.
// region is the AWS region (e.g., "us-east-1"); empty falls back to AWS_REGION,
// AWS_DEFAULT_REGION, then us-east-1.
// accessKey and secretKey are static AWS credentials for SigV4 signing. When
// either is empty, a bearer token from AWS_BEARER_TOKEN_BEDROCK is used if set,
// otherwise the standard AWS credential chain signs requests.
// modelID is the Bedrock model identifier (e.g., "anthropic.claude-sonnet-5-5").
func NewBedrock(region, accessKey, secretKey, modelID string) *Bedrock {
	cfg := ProviderConfig{Model: modelID}
	if accessKey == "" || secretKey == "" {
		return NewBedrockWithConfig(region, cfg, "")
	}
	cfg.APIKey = accessKey
	return NewBedrockWithConfig(region, cfg, secretKey)
}

// NewBedrockWithConfig creates a Bedrock provider with full configuration.
// With a non-empty secretKey, cfg.APIKey is the AWS access key ID and requests
// are SigV4-signed with those static credentials (or the default AWS
// credential chain when cfg.APIKey is empty). With an empty secretKey,
// cfg.APIKey is a Bedrock API key sent as a bearer token, falling back to
// AWS_BEARER_TOKEN_BEDROCK and then to SigV4 with the default credential
// chain. cfg.BaseURL overrides the regional endpoint (tests, VPC endpoints).
func NewBedrockWithConfig(region string, cfg ProviderConfig, secretKey string) *Bedrock {
	region = bedrockRegion(region)
	if secretKey != "" {
		accessKey := cfg.APIKey
		cfg.APIKey = ""
		if accessKey == "" {
			return newBedrock(region, cfg, newBedrockDefaultCredentials(region))
		}
		return newBedrock(region, cfg, credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""))
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	}
	if cfg.APIKey != "" {
		return newBedrock(region, cfg, nil)
	}
	return newBedrock(region, cfg, newBedrockDefaultCredentials(region))
}

// newBedrock builds the provider. A nil creds authenticates with cfg.APIKey as
// a bearer token; otherwise every request attempt is SigV4-signed with creds.
func newBedrock(region string, cfg ProviderConfig, creds aws.CredentialsProvider) *Bedrock {
	if cfg.BaseURL == "" {
		cfg.BaseURL = fmt.Sprintf("https://bedrock-mantle.%s.api.aws/anthropic", region)
	}
	if cfg.Model == "" {
		cfg.Model = bedrockDefaultModel
	}
	headers := map[string]string{"anthropic-version": "2023-06-01"}
	b := &Bedrock{region: region}
	var opts []httpOption
	if creds == nil {
		headers["x-api-key"] = cfg.APIKey
	} else {
		cfg.APIKey = ""
		b.sigv4 = &bedrockSigV4{creds: creds, signer: v4.NewSigner(), region: region, now: time.Now}
		opts = append(opts, withRequestSigner(b.sigv4.sign))
	}
	b.Anthropic = newAnthropicProvider(cfg, headers, opts...)
	return b
}

// bedrockRegion resolves the endpoint region: the explicit value, then
// AWS_REGION, then AWS_DEFAULT_REGION, then us-east-1.
func bedrockRegion(region string) string {
	for _, r := range []string{region, os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION")} {
		if r != "" {
			return r
		}
	}
	return bedrockDefaultRegion
}

func (b *Bedrock) Name() string { return "bedrock" }

func (b *Bedrock) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	resp, err := b.Anthropic.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("bedrock: %w", err)
	}
	return resp, nil
}

func (b *Bedrock) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	ch, err := b.Anthropic.StreamChat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("bedrock: %w", err)
	}
	return ch, nil
}

// bedrockSigV4 signs Mantle requests with AWS Signature Version 4.
type bedrockSigV4 struct {
	creds  aws.CredentialsProvider
	signer *v4.Signer
	region string
	// now supplies the signing time; injectable for tests.
	now func() time.Time
}

// sign authenticates req with credentials resolved at send time, so expiring
// credentials refresh and each retry gets a fresh timestamp. The payload hash
// covers the exact body bytes sent.
func (s *bedrockSigV4) sign(req *http.Request, body []byte) error {
	ctx := req.Context()
	creds, err := s.creds.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("bedrock credentials: %w", err)
	}
	req.Header.Del("x-api-key")
	sum := sha256.Sum256(body)
	if err := s.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), bedrockSigningName, s.region, s.now()); err != nil {
		return fmt.Errorf("bedrock sigv4: %w", err)
	}
	return nil
}

// newBedrockDefaultCredentials returns the standard AWS credential chain (env,
// shared config and SSO, assumed roles, ECS, IMDS), loaded on first use so
// construction does no I/O, and cached until the credentials expire.
func newBedrockDefaultCredentials(region string) aws.CredentialsProvider {
	return aws.NewCredentialsCache(&bedrockDefaultCredentials{region: region})
}

// bedrockDefaultCredentials loads the AWS default config lazily. A failed load
// is not remembered, so a later request retries it.
type bedrockDefaultCredentials struct {
	region string
	mu     sync.Mutex
	chain  aws.CredentialsProvider
}

func (d *bedrockDefaultCredentials) Retrieve(ctx context.Context) (aws.Credentials, error) {
	d.mu.Lock()
	if d.chain == nil {
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(d.region))
		if err != nil {
			d.mu.Unlock()
			return aws.Credentials{}, fmt.Errorf("load aws config: %w", err)
		}
		if cfg.Credentials == nil {
			d.mu.Unlock()
			return aws.Credentials{}, errors.New("load aws config: no credential provider")
		}
		d.chain = cfg.Credentials
	}
	chain := d.chain
	d.mu.Unlock()
	return chain.Retrieve(ctx)
}
