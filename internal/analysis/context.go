package analysis

import (
	"context"
	"strings"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

const (
	maxContextFileBytes  = 20000
	maxContextFiles      = 12
	maxInstructionsBytes = 30000
)

// BuildContext fetches full-file context for up to maxContextFiles kept files.
func BuildContext(ctx context.Context, gh domain.GitHubAPI, owner, repo, ref string, files []domain.ChangedFile) map[string]string {
	out := map[string]string{}
	count := 0
	for _, f := range files {
		if count >= maxContextFiles {
			break
		}
		if f.Status != "modified" && f.Status != "added" {
			continue
		}
		content, err := gh.GetFile(ctx, owner, repo, f.Path, ref)
		if err != nil || content == "" || len(content) > maxContextFileBytes {
			continue
		}
		out[f.Path] = content
		count++
	}
	return out
}

// BuildInstructions concatenates repo instruction files (AGENTS.md etc.).
func BuildInstructions(ctx context.Context, gh domain.GitHubAPI, owner, repo, ref string, instructionFiles []string) string {
	files := instructionFiles
	if len(files) == 0 {
		files = []string{"AGENTS.md", "CLAUDE.md"}
	}
	var b strings.Builder
	for _, name := range files[:min(3, len(files))] {
		content, err := gh.GetFile(ctx, owner, repo, name, ref)
		if err != nil || content == "" {
			continue
		}
		b.WriteString("### ")
		b.WriteString(name)
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n\n")
		if b.Len() > maxInstructionsBytes {
			break
		}
	}
	out := b.String()
	if len(out) > maxInstructionsBytes {
		out = out[:maxInstructionsBytes]
	}
	return out
}
