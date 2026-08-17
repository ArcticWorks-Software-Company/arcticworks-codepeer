package llm

import (
	"fmt"
	"log/slog"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

// reviewSchema is the strict structured-output JSON schema for the review
// result. It uses only keywords compatible with strict structured outputs.
const reviewSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["summary", "status", "findings"],
  "properties": {
    "summary": {
      "type": "string",
      "description": "2-4 sentence plain-language overview of the change and overall assessment."
    },
    "status": {
      "type": "string",
      "enum": ["approved", "changes_requested", "no_findings"],
      "description": "Overall verdict. no_findings means the diff is clean - do not invent issues."
    },
    "findings": {
      "type": "array",
      "description": "Ordered by severity descending. Empty array is valid and expected.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "file", "line", "severity", "category", "title", "body", "suggestion", "confidence", "actionable"],
        "properties": {
          "id": {"type": "string"},
          "file": {"type": "string", "description": "Exact path from the provided diff."},
          "line": {
            "type": ["integer", "null"],
            "description": "New-side line number in the head commit, from the diff hunk ranges. null when file-level."
          },
          "severity": {"type": "string", "enum": ["critical", "high", "medium", "low", "nit"]},
          "category": {"type": "string", "enum": ["bug", "security", "performance", "correctness", "test", "maintainability", "style", "other"]},
          "title": {"type": "string", "description": "One-line imperative summary."},
          "body": {"type": "string", "description": "Why this is a problem, quoting exact lines from the diff as evidence."},
          "suggestion": {
            "anyOf": [
              {
                "type": "object",
                "additionalProperties": false,
                "required": ["old", "new"],
                "properties": {
                  "old": {"type": "string", "description": "Exact existing snippet copied verbatim from the diff"},
                  "new": {"type": "string", "description": "Exact replacement snippet"}
                }
              },
              {"type": "null"}
            ]
          },
          "confidence": {"type": "number", "description": "0.0-1.0"},
          "actionable": {"type": "boolean", "description": "true only if the author can act on it in this PR"}
        }
      }
    }
  }
}`

func validateResult(r *domain.ReviewResult) error {
	switch r.Status {
	case domain.StatusApproved, domain.StatusChangesRequested, domain.StatusNoFindings:
	default:
		return fmt.Errorf("llm: invalid review status %q", r.Status)
	}
	kept := make([]domain.Finding, 0, len(r.Findings))
	for _, f := range r.Findings {
		if !f.Actionable {
			slog.Warn("llm: dropping non-actionable finding", "id", f.ID)
			continue
		}
		if f.File == "" {
			slog.Warn("llm: dropping finding with empty file", "id", f.ID)
			continue
		}
		if f.Title == "" || f.Body == "" {
			slog.Warn("llm: dropping finding with empty title or body", "id", f.ID)
			continue
		}
		if !validSeverity(f.Severity) {
			slog.Warn("llm: dropping finding with invalid severity", "id", f.ID, "severity", f.Severity)
			continue
		}
		if !validCategory(f.Category) {
			slog.Warn("llm: dropping finding with invalid category", "id", f.ID, "category", f.Category)
			continue
		}
		if f.Confidence < 0 {
			f.Confidence = 0
		} else if f.Confidence > 1 {
			f.Confidence = 1
		}
		if f.Suggestion != nil && (f.Suggestion.Old == "" || f.Suggestion.New == "") {
			slog.Warn("llm: dropping suggestion with empty old/new snippet", "id", f.ID)
			f.Suggestion = nil
		}
		kept = append(kept, f)
	}
	r.Findings = kept
	return nil
}

func validSeverity(s domain.Severity) bool {
	switch s {
	case domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow, domain.SeverityNit:
		return true
	}
	return false
}

func validCategory(c domain.Category) bool {
	switch c {
	case domain.CategoryBug, domain.CategorySecurity, domain.CategoryPerformance, domain.CategoryCorrectness, domain.CategoryTest, domain.CategoryMaintainability, domain.CategoryStyle, domain.CategoryOther:
		return true
	}
	return false
}
