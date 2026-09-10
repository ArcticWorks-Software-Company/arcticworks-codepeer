package analysis

import (
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

// agentSpec describes one specialist review agent.
type agentSpec struct {
	Name     string
	Category domain.Category
	Focus    string
}

// agentSpecs maps configured agent names to their prompt specializations.
// Unknown names are ignored. Empty input returns nil (legacy general pass).
func agentSpecs(names []string) []agentSpec {
	if len(names) == 0 {
		return nil
	}
	all := map[string]agentSpec{
		"security": {
			Name:     "security",
			Category: domain.CategorySecurity,
			Focus: `You are the SECURITY expert on this review team.
Look only at security. Report injection (SQL, command, template, code), hardcoded secrets or credentials, broken authentication or authorization, unsafe use of untrusted input, path traversal, unsafe deserialization, weak cryptography, and injection into UI markup.
Do not report style, performance, or general bug hygiene. Other specialists cover them.
Report only actionable security issues. Use category "security". If you find nothing, return no_findings.`,
		},
		"correctness": {
			Name:     "correctness",
			Category: domain.CategoryBug,
			Focus: `You are the CORRECTNESS expert on this review team.
Look only at functional defects. Report logic errors, off-by-one and boundary errors, mishandled or swallowed errors, null or undefined access, race conditions, unsynchronized shared state, resource leaks, a missing close, release or defer, wrong API use, and behavior regressions.
Look for the subtle defects that other reviewers miss. A refactor can hide a regression.
Report only actionable correctness issues. Use category "bug". If you find nothing, return no_findings.`,
		},
		"performance": {
			Name:     "performance",
			Category: domain.CategoryPerformance,
			Focus: `You are the PERFORMANCE expert on this review team.
Look only at performance. Report allocations in hot paths, needless copies, blocking I/O or heavy work on a UI thread, quadratic or worse complexity in loops, unbounded growth, and repeated computation of the same value.
Report a magic constant only if it can degrade runtime behavior.
Report only actionable performance issues. Use category "performance". If you find nothing, return no_findings.`,
		},
		"maintainability": {
			Name:     "maintainability",
			Category: domain.CategoryMaintainability,
			Focus: `You are the MAINTAINABILITY expert on this review team.
Look only at long-term code health. Report a design that does not fit the system, over-engineering, unused generality, dead code, confusing names, missing or wrong comments, missing tests for new behavior, and code that breaks the conventions around it.
Report only actionable maintainability issues. Use category "maintainability", "test" or "style". If you find nothing, return no_findings.`,
		},
		"ux": {
			Name:     "ux",
			Category: domain.CategoryOther,
			Focus: `You are the UX and ACCESSIBILITY expert on this review team.
Look only at user-facing behavior. Report accessibility defects (focus order, labels, contrast), hardcoded visual values that bypass design tokens, input and interaction regressions (offsets, sensitivity, thresholds), misleading states, and anything a user sees or feels as broken.
For UI code, check that a behavior change matches its stated intent.
Report only actionable UX issues. Use category "other". If you find nothing, return no_findings.`,
		},
	}
	out := make([]agentSpec, 0, len(names))
	for _, n := range names {
		if s, ok := all[n]; ok {
			out = append(out, s)
		}
	}
	return out
}
