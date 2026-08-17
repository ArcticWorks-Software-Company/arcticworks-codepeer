package posting

import (
	"strconv"
	"strings"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

func severityTag(s string) string {
	switch s {
	case "critical":
		return "**Critical**"
	case "high":
		return "**High**"
	case "medium":
		return "**Medium**"
	case "low":
		return "**Low**"
	case "nit":
		return "**Nit**"
	default:
		return "**Unknown**"
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func lineLabel(line int) string {
	if line <= 0 {
		return "-"
	}
	return strconv.Itoa(line)
}

func verdictText(status domain.ReviewStatus) string {
	switch status {
	case domain.StatusApproved:
		return "No blocking findings."
	case domain.StatusChangesRequested:
		return "Findings below should be addressed."
	default:
		return "No findings — nothing to act on."
	}
}

func sanitizeFence(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.TrimRight(line, "\r") == "```" {
			lines[i] = "````"
		}
	}
	return strings.Join(lines, "\n")
}

func capString(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// BuildSummaryBody renders the summary review body for a PR analysis.
func BuildSummaryBody(out ReviewOutput) string {
	var sb strings.Builder
	sb.WriteString("## CodePeer Review\n\n")
	sb.WriteString(out.Result.Summary)
	if len(out.PartialFiles) > 0 {
		sb.WriteString("\n\n### Partial review\n\nAnalysis failed for these changed files (LLM errors); they were NOT reviewed:\n")
		for _, f := range out.PartialFiles {
			sb.WriteString("\n- `" + f + "`")
		}
		sb.WriteString("\n\n**Verdict:** Partial review — do not rely on this as a complete review.")
	} else {
		sb.WriteString("\n\n**Verdict:** " + verdictText(out.Result.Status))
	}
	n := len(out.Findings)
	sb.WriteString("\n\n### Findings (" + strconv.Itoa(n) + ")")
	if n == 0 {
		sb.WriteString("\n\nNo findings — nothing to act on.")
	} else {
		sb.WriteString("\n| Severity | File | Title |\n|---|---|---|")
		for _, f := range out.Findings {
			sb.WriteString("\n| " + severityTag(f.Severity) + " | `" + f.File + ":" + lineLabel(f.Line) + "` | " + f.Title + " |")
		}
	}
	s := sb.String()
	if len(s) > 65000 {
		return s[:65000] + "\n\n_(truncated)_"
	}
	return s
}

// BuildCommentBody renders one inline review comment body.
func BuildCommentBody(f domain.FindingRecord) string {
	var sb strings.Builder
	sb.WriteString("**" + capitalize(f.Severity) + " (" + f.Category + ")** " + f.Title)
	sb.WriteString("\n\n")
	sb.WriteString(f.Body)
	if f.Suggestion != nil && f.Suggestion.Old != "" && f.Suggestion.New != "" {
		sb.WriteString("\n\nSuggested fix:\n```suggestion\n")
		sb.WriteString(sanitizeFence(f.Suggestion.New))
		sb.WriteString("\n```")
	}
	return capString(sb.String(), 30000)
}

// BuildIssueBody renders the body of a standalone analysis issue.
func BuildIssueBody(out ReviewOutput, f domain.FindingRecord, prNumber int, repoOwner, repoName string) string {
	var sb strings.Builder
	sb.WriteString("**" + capitalize(f.Severity) + " (" + f.Category + ")** " + f.Title)
	sb.WriteString("\n\nSeverity: " + f.Severity)
	sb.WriteString("\nCategory: " + f.Category)
	sb.WriteString("\nFile: `" + f.File + ":" + lineLabel(f.Line) + "`")
	sb.WriteString("\n\n" + f.Body)
	if f.Suggestion != nil && f.Suggestion.Old != "" && f.Suggestion.New != "" {
		sb.WriteString("\n\nSuggested fix:\n```suggestion\n")
		sb.WriteString(sanitizeFence(f.Suggestion.New))
		sb.WriteString("\n```")
	}
	if prNumber > 0 {
		sb.WriteString("\n\nFound in PR: " + repoOwner + "/" + repoName + "#" + strconv.Itoa(prNumber))
	} else {
		sb.WriteString("\n\nFound in push to " + repoName)
	}
	return capString(sb.String(), 30000)
}

// BuildPushCommentBody renders one combined rolling-issue comment for a push.
func BuildPushCommentBody(fs []domain.FindingRecord, headSHA string) string {
	var sb strings.Builder
	for i, f := range fs {
		if i >= 10 {
			break
		}
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString("**" + capitalize(f.Severity) + " " + f.Category + "** " + f.Title + " — `" + f.File + ":" + lineLabel(f.Line) + "`")
		sb.WriteString("\n\n" + f.Body)
		if f.Suggestion != nil && f.Suggestion.Old != "" && f.Suggestion.New != "" {
			sb.WriteString("\n\nSuggested fix:\n```suggestion\n")
			sb.WriteString(sanitizeFence(f.Suggestion.New))
			sb.WriteString("\n```")
		}
	}
	sha := headSHA
	if len(sha) > 8 {
		sha = sha[:8]
	}
	sb.WriteString("\n\nPush: `" + sha + "`")
	return capString(sb.String(), 30000)
}

func issueTitle(f domain.FindingRecord) string {
	t := "CodePeer: " + f.Title
	if len(t) > 200 {
		return t[:200]
	}
	return t
}
