package guardrails

import (
	"fmt"
	"regexp"
	"strings"
)

// InjectionGuardrail detects common prompt injection patterns.
type InjectionGuardrail struct {
	Sensitivity int // 1=low (obvious attacks), 2=medium, 3=high (aggressive)
}

var injectionPatternsLow = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`),
	regexp.MustCompile(`(?i)disregard\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`),
	regexp.MustCompile(`(?i)forget\s+(all\s+)?(previous|prior|above|your)\s+(instructions?|prompts?|rules?|training)`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+[a-z]+`),
	regexp.MustCompile(`(?i)new\s+(system\s+)?prompt:`),
	regexp.MustCompile(`(?i)system:\s*you\s+are`),
}

var injectionPatternsMedium = []*regexp.Regexp{
	regexp.MustCompile(`(?i)pretend\s+(you\s+are|to\s+be)\s+`),
	regexp.MustCompile(`(?i)act\s+as\s+(if\s+you\s+are|a)\s+`),
	regexp.MustCompile(`(?i)role\s*play\s+as\s+`),
	regexp.MustCompile(`(?i)<\s*system\s*>`),
	regexp.MustCompile(`(?i)\[\s*SYSTEM\s*\]`),
	regexp.MustCompile(`(?i)override\s+(safety|content|output)\s+(filter|policy|rules?)`),
}

var injectionPatternsHigh = []*regexp.Regexp{
	regexp.MustCompile(`(?i)reveal\s+(your|the)\s+(system\s+)?(prompt|instructions?|rules?)`),
	regexp.MustCompile(`(?i)what\s+(are|is)\s+your\s+(system\s+)?(prompt|instructions?|rules?)`),
	regexp.MustCompile(`(?i)print\s+(your|the)\s+(system\s+)?(prompt|instructions?)`),
	regexp.MustCompile(`(?i)output\s+(your|the)\s+(system\s+)?(prompt|instructions?)`),
}

// Check scans content for prompt injection patterns.
func (g *InjectionGuardrail) Check(_ interface{}, content string) *Result {
	sensitivity := g.Sensitivity
	if sensitivity == 0 {
		sensitivity = 2
	}

	var patterns []*regexp.Regexp
	patterns = append(patterns, injectionPatternsLow...)
	if sensitivity >= 2 {
		patterns = append(patterns, injectionPatternsMedium...)
	}
	if sensitivity >= 3 {
		patterns = append(patterns, injectionPatternsHigh...)
	}

	lower := strings.ToLower(content)
	for _, p := range patterns {
		if p.MatchString(lower) {
			return &Result{
				Passed: false,
				Reason: fmt.Sprintf("potential prompt injection detected"),
			}
		}
	}

	return nil
}
