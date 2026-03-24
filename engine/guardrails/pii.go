package guardrails

import (
	"fmt"
	"regexp"
	"strings"
)

// PIIType identifies the kind of personally identifiable information detected.
type PIIType string

const (
	PIIEmail      PIIType = "email"
	PIIPhone      PIIType = "phone"
	PIISSN        PIIType = "ssn"
	PIICreditCard PIIType = "credit_card"
	PIIIPAddress  PIIType = "ip_address"
)

// PIIGuardrail detects and optionally redacts personally identifiable information.
type PIIGuardrail struct {
	DetectTypes []PIIType
	Redact      bool
}

var piiPatterns = map[PIIType]*regexp.Regexp{
	PIIEmail:      regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
	PIIPhone:      regexp.MustCompile(`(?:\+?1[-.\s]?)?(?:\(?\d{3}\)?[-.\s]?)?\d{3}[-.\s]?\d{4}`),
	PIISSN:        regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
	PIICreditCard: regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
	PIIIPAddress:  regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
}

// Check scans content for PII patterns and returns a result if any are found.
func (g *PIIGuardrail) Check(_ interface{}, content string) *Result {
	types := g.DetectTypes
	if len(types) == 0 {
		types = []PIIType{PIIEmail, PIIPhone, PIISSN, PIICreditCard, PIIIPAddress}
	}

	var found []PIIType
	for _, pt := range types {
		pattern, ok := piiPatterns[pt]
		if !ok {
			continue
		}
		if pattern.MatchString(content) {
			found = append(found, pt)
		}
	}

	if len(found) == 0 {
		return nil
	}

	typeStrs := make([]string, len(found))
	for i, f := range found {
		typeStrs[i] = string(f)
	}

	return &Result{
		Passed: false,
		Reason: fmt.Sprintf("PII detected: %s", strings.Join(typeStrs, ", ")),
	}
}

// RedactPII replaces detected PII patterns with redaction markers.
func RedactPII(content string, types []PIIType) string {
	if len(types) == 0 {
		types = []PIIType{PIIEmail, PIIPhone, PIISSN, PIICreditCard, PIIIPAddress}
	}
	for _, pt := range types {
		pattern, ok := piiPatterns[pt]
		if !ok {
			continue
		}
		content = pattern.ReplaceAllString(content, fmt.Sprintf("[REDACTED_%s]", strings.ToUpper(string(pt))))
	}
	return content
}
