package guardrails

import (
	"strings"
	"testing"
)

func TestPIIGuardrail_DetectsEmail(t *testing.T) {
	g := &PIIGuardrail{DetectTypes: []PIIType{PIIEmail}}
	result := g.Check(nil, "Contact me at user@example.com")
	if result == nil {
		t.Fatal("expected PII detection for email")
	}
	if !strings.Contains(result.Reason, "email") {
		t.Errorf("reason should mention email: %s", result.Reason)
	}
}

func TestPIIGuardrail_DetectsSSN(t *testing.T) {
	g := &PIIGuardrail{DetectTypes: []PIIType{PIISSN}}
	result := g.Check(nil, "My SSN is 123-45-6789")
	if result == nil {
		t.Fatal("expected PII detection for SSN")
	}
}

func TestPIIGuardrail_DetectsPhone(t *testing.T) {
	g := &PIIGuardrail{DetectTypes: []PIIType{PIIPhone}}
	result := g.Check(nil, "Call me at 555-123-4567")
	if result == nil {
		t.Fatal("expected PII detection for phone")
	}
}

func TestPIIGuardrail_DetectsIP(t *testing.T) {
	g := &PIIGuardrail{DetectTypes: []PIIType{PIIIPAddress}}
	result := g.Check(nil, "Server at 192.168.1.1")
	if result == nil {
		t.Fatal("expected PII detection for IP address")
	}
}

func TestPIIGuardrail_NoMatch(t *testing.T) {
	g := &PIIGuardrail{DetectTypes: []PIIType{PIIEmail, PIISSN}}
	result := g.Check(nil, "This is clean text without any PII")
	if result != nil {
		t.Errorf("expected nil result for clean text, got: %s", result.Reason)
	}
}

func TestPIIGuardrail_AllTypesDefault(t *testing.T) {
	g := &PIIGuardrail{}
	result := g.Check(nil, "Email: test@example.com, SSN: 123-45-6789")
	if result == nil {
		t.Fatal("expected PII detection")
	}
}

func TestRedactPII_Email(t *testing.T) {
	result := RedactPII("Contact user@example.com now", []PIIType{PIIEmail})
	if strings.Contains(result, "user@example.com") {
		t.Error("email should be redacted")
	}
	if !strings.Contains(result, "[REDACTED_EMAIL]") {
		t.Error("should contain redaction marker")
	}
}

func TestRedactPII_SSN(t *testing.T) {
	result := RedactPII("SSN: 123-45-6789", []PIIType{PIISSN})
	if strings.Contains(result, "123-45-6789") {
		t.Error("SSN should be redacted")
	}
}

func TestRedactPII_AllTypes(t *testing.T) {
	result := RedactPII("Email: a@b.com, IP: 10.0.0.1", nil)
	if strings.Contains(result, "a@b.com") {
		t.Error("email should be redacted")
	}
	if strings.Contains(result, "10.0.0.1") {
		t.Error("IP should be redacted")
	}
}

func TestPIIGuardrail_UnknownType(t *testing.T) {
	// Use an unknown PII type that has no pattern
	g := &PIIGuardrail{DetectTypes: []PIIType{"unknown_type"}}
	result := g.Check(nil, "any content here")
	// Should not panic, should return nil since unknown type has no pattern
	if result != nil {
		t.Errorf("expected nil result for unknown type, got: %v", result)
	}
}

func TestPIIGuardrail_DetectsCreditCard(t *testing.T) {
	g := &PIIGuardrail{DetectTypes: []PIIType{PIICreditCard}}
	result := g.Check(nil, "Card number: 4111111111111111")
	if result == nil {
		t.Fatal("expected PII detection for credit card")
	}
}

func TestRedactPII_UnknownType(t *testing.T) {
	// Should not panic for unknown type
	result := RedactPII("some content", []PIIType{"unknown_type"})
	if result != "some content" {
		t.Errorf("unknown type should not modify content, got: %s", result)
	}
}
