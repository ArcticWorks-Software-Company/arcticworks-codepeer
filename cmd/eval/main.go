// Command eval measures CodePeer review quality against a seeded-defect
// dataset. It requires LLM_API_KEY (plus optional LLM_BASE_URL/LLM_MODEL) and
// calls the real model, reporting precision and recall per case.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/llm"
)

type defect struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
}

type testCase struct {
	Name     string               `json:"name"`
	Language string               `json:"language"`
	Diff     string               `json:"diff"`
	Files    []domain.ChangedFile `json:"files"`
	Defects  []defect             `json:"defects"`
}

func main() {
	ctx := context.Background()
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Println("LLM_API_KEY is not set; skipping evaluation.")
		fmt.Println("Set LLM_API_KEY (and optionally LLM_BASE_URL, LLM_MODEL) to run against DeepSeek.")
		os.Exit(0)
	}

	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "deepseek-v4-flash"
	}
	effort := os.Getenv("LLM_REASONING_EFFORT")
	if effort == "" {
		effort = "high"
	}

	client := llm.New(llm.Config{
		BaseURL:         baseURL,
		APIKey:          apiKey,
		Model:           model,
		ReasoningEffort: effort,
		Timeout:         10 * time.Minute,
	})

	dir := "testdata/eval/cases"
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		os.Exit(1)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var cases []testCase
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "eval: %v\n", err)
			os.Exit(1)
		}
		var tc testCase
		if err := json.Unmarshal(raw, &tc); err != nil {
			fmt.Fprintf(os.Stderr, "eval: %s: %v\n", e.Name(), err)
			os.Exit(1)
		}
		cases = append(cases, tc)
	}

	header := "case | defects | found | false+ | recall | precision"
	fmt.Println(header)
	fmt.Println("-----|---------|-------|-------|--------|-----------")
	totalDefects, totalFound, totalExtra := 0, 0, 0
	for _, tc := range cases {
		res, err := client.Review(ctx, domain.ReviewRequest{
			RepoOwner: "eval",
			RepoName:  tc.Name,
			PRNumber:  1,
			PRTitle:   "seeded defects",
			HeadSHA:   "eval",
			Diff:      tc.Diff,
			Files:     tc.Files,
			Config: domain.ReviewConfig{
				Strictness:  "balanced",
				MaxFindings: 10,
				PerFileCap:  5,
			},
		})
		if err != nil {
			fmt.Printf("%s | ERROR: %v\n", tc.Name, err)
			continue
		}
		matched := map[int]bool{}
		for _, f := range res.Findings {
			for i, d := range tc.Defects {
				if f.File == d.File && math.Abs(float64(f.Line-d.Line)) <= 3 {
					matched[i] = true
				}
			}
		}
		found := len(matched)
		extra := len(res.Findings) - found
		if extra < 0 {
			extra = 0
		}
		totalDefects += len(tc.Defects)
		totalFound += found
		totalExtra += extra
		recall := float64(found) / float64(len(tc.Defects))
		precision := 0.0
		if len(res.Findings) > 0 {
			precision = float64(found) / float64(len(res.Findings))
		}
		fmt.Printf("%s | %d | %d | %d | %.2f | %.2f\n", tc.Name, len(tc.Defects), found, extra, recall, precision)
		for i, d := range tc.Defects {
			if !matched[i] {
				fmt.Printf("    missed: %s:%d %s (%s)\n", d.File, d.Line, d.Description, d.Severity)
			}
		}
	}
	fmt.Println("-----|---------|-------|-------|--------|-----------")
	recall := 0.0
	if totalDefects > 0 {
		recall = float64(totalFound) / float64(totalDefects)
	}
	precision := 0.0
	if totalFound+totalExtra > 0 {
		precision = float64(totalFound) / float64(totalFound+totalExtra)
	}
	fmt.Printf("TOTAL | %d | %d | %d | %.2f | %.2f\n", totalDefects, totalFound, totalExtra, recall, precision)
}
