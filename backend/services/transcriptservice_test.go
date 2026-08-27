package services

import (
	"context"
	"strings"
	"testing"

	"arguehub/models"
)

const validHumanDebateJudgment = `{
  "opening_statement": {
    "for": {"score": 8, "reason": "Strong opening."},
    "against": {"score": 7, "reason": "Clear opposing position."}
  },
  "cross_examination_questions": {
    "for": {"score": 7, "reason": "Relevant questions."},
    "against": {"score": 6, "reason": "Mostly relevant questions."}
  },
  "cross_examination_answers": {
    "for": {"score": 8, "reason": "Direct answers."},
    "against": {"score": 7, "reason": "Generally direct answers."}
  },
  "closing": {
    "for": {"score": 9, "reason": "Persuasive summary."},
    "against": {"score": 8, "reason": "Effective summary."}
  },
  "total": {"for": 32, "against": 28},
  "verdict": {
    "winner": "For",
    "reason": "The For side earned the higher validated score.",
    "congratulations": "Congratulations to the For side.",
    "opponent_analysis": "The Against side should support its claims with more evidence."
  }
}`

func TestBuildHumanVsHumanJudgePromptIncludesTopic(t *testing.T) {
	topic := "Should social media platforms be regulated more strictly?"
	prompt := buildHumanVsHumanJudgePrompt(topic, map[string]string{
		"openingFor":     "Regulation protects users.",
		"openingAgainst": "Regulation can restrict expression.",
	})

	if !strings.Contains(prompt, `Debate Topic: "`+topic+`"`) {
		t.Fatalf("prompt does not contain the debate topic: %s", prompt)
	}
	if !strings.Contains(prompt, "Judge every argument's relevance against the debate topic above") {
		t.Fatal("prompt does not instruct the model to evaluate topic relevance")
	}
}

func TestResolveDebateTopicUsesSubmittedTopic(t *testing.T) {
	topic := resolveDebateTopic(
		context.Background(),
		"unused-room-id",
		models.DebateTranscript{Topic: "  Should AI decisions be regulated?  "},
		models.DebateTranscript{},
	)

	if topic != "Should AI decisions be regulated?" {
		t.Fatalf("unexpected resolved topic: %q", topic)
	}
}

func TestValidateAndNormalizeJudgeResult(t *testing.T) {
	normalized, err := validateAndNormalizeJudgeResult(validHumanDebateJudgment)
	if err != nil {
		t.Fatalf("expected valid judgment, got error: %v", err)
	}
	if !strings.Contains(normalized, `"winner":"For"`) {
		t.Fatalf("normalized judgment has an unexpected winner: %s", normalized)
	}
}

func TestValidateAndNormalizeJudgeResultRejectsInvalidOutput(t *testing.T) {
	tests := []struct {
		name   string
		result string
	}{
		{
			name:   "missing score",
			result: strings.Replace(validHumanDebateJudgment, `"score": 8, "reason": "Strong opening."`, `"reason": "Strong opening."`, 1),
		},
		{
			name:   "score outside range",
			result: strings.Replace(validHumanDebateJudgment, `"score": 8, "reason": "Strong opening."`, `"score": 11, "reason": "Strong opening."`, 1),
		},
		{
			name:   "empty required reason",
			result: strings.Replace(validHumanDebateJudgment, `"reason": "Strong opening."`, `"reason": ""`, 1),
		},
		{
			name:   "incorrect total",
			result: strings.Replace(validHumanDebateJudgment, `"total": {"for": 32, "against": 28}`, `"total": {"for": 31, "against": 28}`, 1),
		},
		{
			name:   "winner conflicts with totals",
			result: strings.Replace(validHumanDebateJudgment, `"winner": "For"`, `"winner": "Against"`, 1),
		},
		{
			name:   "unknown field",
			result: strings.Replace(validHumanDebateJudgment, `"winner": "For"`, `"winner": "For", "confidence": 0.9`, 1),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateAndNormalizeJudgeResult(test.result); err == nil {
				t.Fatal("expected validation to fail")
			}
		})
	}
}

func TestFallbackJudgeResultPassesValidation(t *testing.T) {
	result := buildFallbackJudgeResult(map[string]string{
		"openingFor":           "A short opening for the motion.",
		"openingAgainst":       "A longer opening against the motion with additional supporting context.",
		"crossForQuestion":     "Why should this policy be adopted?",
		"crossAgainstQuestion": "What risks does this policy create?",
		"crossForAnswer":       "It provides measurable public benefits.",
		"crossAgainstAnswer":   "It may create unintended costs.",
		"closingFor":           "The benefits justify adoption.",
		"closingAgainst":       "The risks justify rejection.",
	})

	if _, err := validateAndNormalizeJudgeResult(result); err != nil {
		t.Fatalf("fallback judgment should satisfy the same schema: %v", err)
	}
}
