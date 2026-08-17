package analysis

import (
	"sort"
	"strings"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

const maxLinesPerChunk = 1500

// Chunk is a group of files reviewed in one LLM call.
type Chunk struct {
	Files    []domain.ChangedFile
	DiffText string
}

// ChunkFiles groups files into chunks whose total changed-line count stays
// within maxLinesPerChunk. A single oversized file gets its own chunk.
func ChunkFiles(files []domain.ChangedFile, diffByFile map[string]string) []Chunk {
	ordered := make([]domain.ChangedFile, len(files))
	copy(ordered, files)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Additions > ordered[j].Additions
	})

	var chunks []Chunk
	var cur Chunk
	curLines := 0
	flush := func() {
		if len(cur.Files) > 0 {
			chunks = append(chunks, cur)
		}
		cur = Chunk{}
		curLines = 0
	}
	for _, f := range ordered {
		lines := f.Additions + f.Deletions
		if lines > maxLinesPerChunk {
			flush()
			chunks = append(chunks, Chunk{Files: []domain.ChangedFile{f}, DiffText: buildDiffText([]domain.ChangedFile{f}, diffByFile)})
			continue
		}
		if len(cur.Files) > 0 && curLines+lines > maxLinesPerChunk {
			flush()
		}
		cur.Files = append(cur.Files, f)
		curLines += lines
	}
	flush()
	for i := range chunks {
		if chunks[i].DiffText == "" {
			chunks[i].DiffText = buildDiffText(chunks[i].Files, diffByFile)
		}
	}
	return chunks
}

func buildDiffText(files []domain.ChangedFile, diffByFile map[string]string) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString("\n--- FILE: ")
		b.WriteString(f.Path)
		b.WriteString(" ---\n")
		if t, ok := diffByFile[f.Path]; ok {
			b.WriteString(t)
		}
		b.WriteString("\n")
	}
	return b.String()
}
