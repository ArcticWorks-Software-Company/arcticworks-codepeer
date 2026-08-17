package analysis

import (
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/bmatcuk/doublestar/v4"
)

// DefaultIgnorePatterns are always-skipped paths regardless of repo config.
var DefaultIgnorePatterns = []string{
	"**/*.lock",
	"**/package-lock.json",
	"**/yarn.lock",
	"**/pnpm-lock.yaml",
	"**/*.min.js",
	"**/*.min.css",
	"**/*.map",
	"**/*.pb.go",
	"**/vendor/**",
	"**/node_modules/**",
	"**/dist/**",
	"**/__generated__/**",
	"**/*.generated.*",
	"**/*.png",
	"**/*.jpg",
	"**/*.jpeg",
	"**/*.gif",
	"**/*.svg",
	"**/*.ico",
	"**/*.woff*",
	"**/*.ttf",
	"**/*.eot",
	"**/*.zip",
	"**/*.tar*",
	"**/*.gz",
	"**/*.exe",
	"**/*.dll",
	"**/*.so",
	"**/*.dylib",
	"**/*.class",
	"**/*.jar",
	"**/*.bin",
	"**/*.pdf",
}

// ShouldSkipFile reports whether a path matches the built-in ignore list.
func ShouldSkipFile(path string) bool {
	for _, p := range DefaultIgnorePatterns {
		if ok, err := doublestar.Match(p, path); err == nil && ok {
			return true
		}
	}
	return false
}

// FilterFiles partitions files into kept and skipped using the built-in
// ignore list plus custom patterns. Patterns that fail to compile are
// ignored.
func FilterFiles(files []domain.ChangedFile, ignore []string) (kept, skipped []domain.ChangedFile) {
	for _, f := range files {
		if ShouldSkipFile(f.Path) || matchesAny(f.Path, ignore) {
			skipped = append(skipped, f)
			continue
		}
		kept = append(kept, f)
	}
	return kept, skipped
}

func matchesAny(path string, patterns []string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if ok, err := doublestar.Match(p, path); err == nil && ok {
			return true
		}
	}
	return false
}
