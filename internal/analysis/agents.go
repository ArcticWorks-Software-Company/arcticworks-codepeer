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
Focus EXCLUSIVELY on security: injection (SQL, command, template, code), hardcoded secrets or credentials, authentication and authorization flaws, unsafe handling of untrusted input, path traversal, unsafe deserialization, use of weak crypto, and injection into UI markup. Ignore style, performance, and general bug hygiene — other specialists cover them.
Report only actionable security issues with category "security". If you find nothing, return no_findings.`,
		},
		"correctness": {
			Name:     "correctness",
			Category: domain.CategoryBug,
			Focus: `You are the CORRECTNESS expert on this review team.
Focus EXCLUSIVELY on functional defects: logic errors, off-by-one and boundary errors, mishandled or swallowed errors, null/nil and undefined access, race conditions and unsynchronized shared state, resource lifecycle bugs (leaks, missing close/release/defer), wrong API usage, and behavioral regressions. Pay special attention to subtle defects other reviewers miss: the diff may hide a regression inside a refactor.
Report only actionable correctness issues with category "bug". If you find nothing, return no_findings.`,
		},
		"performance": {
			Name:     "performance",
			Category: domain.CategoryPerformance,
			Focus: `You are the PERFORMANCE expert on this review team.
Focus EXCLUSIVELY on performance: allocations in hot paths, needless copies, blocking I/O or heavy work on UI threads, quadratic or worse complexity in loops, unbounded growth, and wasteful recomputation. Flag magic constants only when they plausibly degrade runtime behavior.
Report only actionable performance issues with category "performance". If you find nothing, return no_findings.`,
		},
		"maintainability": {
			Name:     "maintainability",
			Category: domain.CategoryMaintainability,
			Focus: `You are the MAINTAINABILITY expert on this review team.
Focus EXCLUSIVELY on long-term code health: design coherence and system integration, YAGNI and over-engineering, dead code, confusing naming, missing or misleading comments, missing tests for new behavior, and inconsistency with the surrounding codebase conventions. 
Report only actionable maintainability issues with categories "maintainability", "test", or "style". If you find nothing, return no_findings.`,
		},
		"ux": {
			Name:     "ux",
			Category: domain.CategoryOther,
			Focus: `You are the UX and ACCESSIBILITY expert on this review team.
Focus EXCLUSIVELY on user-facing behavior: accessibility (focus handling, labels, contrast), hardcoded visual values that bypass design tokens, input and interaction regressions (offsets, sensitivity, thresholds), misleading states, and anything a user would see or feel as broken. For UI code, verify that behavior changes match their intent.
Report only actionable UX issues with category "other". If you find nothing, return no_findings.`,
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
