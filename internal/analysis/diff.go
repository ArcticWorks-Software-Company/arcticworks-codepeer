package analysis

import (
	"strconv"
	"strings"
)

// Hunk is one parsed @@ header range in a unified diff.
type Hunk struct {
	NewStart int
	NewCount int
	OldStart int
	OldCount int
	Header   string
}

// ParseHunks parses a unified diff and returns hunks keyed by the new file
// path (falling back to the old path for deleted files).
func ParseHunks(diff string) (map[string][]Hunk, error) {
	out := map[string][]Hunk{}
	var current string
	lines := strings.Split(diff, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "diff --git "):
			current = pathFromDiffHeader(line)
		case strings.HasPrefix(line, "rename from "):
			if current != "" {
				from := strings.TrimSpace(strings.TrimPrefix(line, "rename from "))
				// The rename target will be captured from the +++ header; if we
				// already keyed by the old path, keep it for now.
				if _, ok := out[from]; ok {
					_ = from
				}
			}
		case strings.HasPrefix(line, "--- "):
			if current == "" {
				current = pathFromTripleHeader(line)
			}
		case strings.HasPrefix(line, "+++ "):
			if p := pathFromTripleHeader(line); p != "" {
				current = p
			}
		case strings.HasPrefix(line, "@@ "):
			if current == "" {
				continue
			}
			if h, ok := parseHunkHeader(line); ok {
				out[current] = append(out[current], h)
			}
		}
	}
	return out, nil
}

func pathFromDiffHeader(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	i := strings.Index(rest, " b/")
	if i < 0 {
		return ""
	}
	b := rest[i+3:]
	if j := strings.Index(b, " "); j >= 0 {
		b = b[:j]
	}
	return strings.TrimSuffix(b, "\r")
}

func pathFromTripleHeader(line string) string {
	p := strings.TrimPrefix(line, "+++ ")
	if p == "/dev/null" || strings.HasSuffix(p, " /dev/null") {
		return ""
	}
	p = strings.TrimPrefix(p, "b/")
	if p == "/dev/null" {
		return ""
	}
	if i := strings.Index(p, "\t"); i >= 0 {
		p = p[:i]
	}
	return p
}

func parseHunkHeader(line string) (Hunk, bool) {
	rest := strings.TrimPrefix(line, "@@ ")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return Hunk{}, false
	}
	rest = rest[:end]
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return Hunk{}, false
	}
	oldStart, oldCount, ok1 := parseRange(parts[0])
	newStart, newCount, ok2 := parseRange(parts[1])
	if !ok1 || !ok2 {
		return Hunk{}, false
	}
	if newCount == 0 {
		newCount = 1
	}
	if oldCount == 0 {
		oldCount = 1
	}
	return Hunk{NewStart: newStart, NewCount: newCount, OldStart: oldStart, OldCount: oldCount, Header: line}, true
}

func parseRange(s string) (start, count int, ok bool) {
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	startS, countS, found := strings.Cut(s, ",")
	start, err := strconv.Atoi(startS)
	if err != nil {
		return 0, 0, false
	}
	if !found {
		return start, 1, true
	}
	count, err = strconv.Atoi(countS)
	if err != nil {
		return 0, 0, false
	}
	return start, count, true
}

// NewLineInDiff reports whether line falls in a new-side hunk range.
func NewLineInDiff(hunks []Hunk, line int) bool {
	for _, h := range hunks {
		if h.NewCount > 0 && line >= h.NewStart && line < h.NewStart+h.NewCount {
			return true
		}
	}
	return false
}

// ExtractAddedLines returns the '+' lines of a patch without the prefix,
// skipping '+++' file headers.
func ExtractAddedLines(patch string) []string {
	var out []string
	for _, l := range strings.Split(patch, "\n") {
		if strings.HasPrefix(l, "+++") || !strings.HasPrefix(l, "+") {
			continue
		}
		out = append(out, strings.TrimPrefix(l, "+"))
	}
	return out
}

// SplitDiffByFile splits a raw unified diff into per-file sections keyed by
// the new path.
func SplitDiffByFile(raw string) (map[string]string, error) {
	out := map[string]string{}
	lines := strings.Split(raw, "\n")
	var current string
	var buf []string
	flush := func() {
		if current == "" {
			return
		}
		text := strings.Join(buf, "\n")
		if text != "" {
			out[current] = text
		}
		buf = buf[:0]
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = pathFromDiffHeader(line)
			buf = append(buf, line)
			continue
		}
		if current != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return out, nil
}
