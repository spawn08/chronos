package evals

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/model"
)

// AccuracyEval uses an LLM judge to score the accuracy of agent output
// against expected output.
type AccuracyEval struct {
	EvalName string
	Judge    model.Provider
	Rubric   string
}

func (e *AccuracyEval) Name() string { return e.EvalName }

func (e *AccuracyEval) Run(ctx context.Context, actual, expected string) EvalResult {
	start := time.Now()

	if e.Judge == nil {
		return e.fallbackEval(actual, expected, start)
	}

	rubric := e.Rubric
	if rubric == "" {
		rubric = "Score accuracy from 0.0 to 1.0. Return ONLY a JSON object: {\"score\": <float>, \"explanation\": \"<reason>\"}"
	}

	resp, err := e.Judge.Chat(ctx, &model.ChatRequest{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: rubric},
			{Role: model.RoleUser, Content: fmt.Sprintf("Expected answer:\n%s\n\nActual answer:\n%s\n\nScore the accuracy.", expected, actual)},
		},
		MaxTokens:   200,
		Temperature: 0,
	})
	if err != nil {
		return EvalResult{
			Name:    e.EvalName,
			Score:   0,
			Passed:  false,
			Error:   err.Error(),
			Latency: time.Since(start),
		}
	}

	score, explanation := parseJudgeResponse(resp.Content)
	return EvalResult{
		Name:       e.EvalName,
		Score:      score,
		Passed:     score >= 0.7,
		Details:    explanation,
		Latency:    time.Since(start),
		TokensUsed: resp.Usage.PromptTokens + resp.Usage.CompletionTokens,
	}
}

func (e *AccuracyEval) fallbackEval(actual, expected string, start time.Time) EvalResult {
	actual = strings.TrimSpace(strings.ToLower(actual))
	expected = strings.TrimSpace(strings.ToLower(expected))

	if actual == expected {
		return EvalResult{Name: e.EvalName, Score: 1.0, Passed: true, Details: "exact match", Latency: time.Since(start)}
	}
	if strings.Contains(actual, expected) {
		return EvalResult{Name: e.EvalName, Score: 0.8, Passed: true, Details: "contains expected", Latency: time.Since(start)}
	}

	commonWords := countCommonWords(actual, expected)
	expectedWords := len(strings.Fields(expected))
	score := 0.0
	if expectedWords > 0 {
		score = float64(commonWords) / float64(expectedWords)
		if score > 1.0 {
			score = 1.0
		}
	}
	return EvalResult{
		Name:    e.EvalName,
		Score:   score,
		Passed:  score >= 0.7,
		Details: fmt.Sprintf("word overlap: %d/%d", commonWords, expectedWords),
		Latency: time.Since(start),
	}
}

func countCommonWords(a, b string) int {
	wordsA := make(map[string]bool)
	for _, w := range strings.Fields(a) {
		wordsA[w] = true
	}
	count := 0
	for _, w := range strings.Fields(b) {
		if wordsA[w] {
			count++
		}
	}
	return count
}

func parseJudgeResponse(content string) (score float64, explanation string) {
	content = strings.TrimSpace(content)
	explanation = content

	idx := strings.Index(content, "\"score\"")
	if idx < 0 {
		return 0.5, explanation
	}

	rest := content[idx+7:]
	rest = strings.TrimLeft(rest, ": ")
	_, _ = fmt.Sscanf(rest, "%f", &score)

	expIdx := strings.Index(content, "\"explanation\"")
	if expIdx >= 0 {
		expRest := content[expIdx+13:]
		expRest = strings.TrimLeft(expRest, ": \"")
		end := strings.Index(expRest, "\"")
		if end > 0 {
			explanation = expRest[:end]
		}
	}

	return score, explanation
}
