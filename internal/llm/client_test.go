package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

const cannedOK = `{"status":"completed","output_text":"{\"summary\":\"ok\",\"status\":\"no_findings\",\"findings\":[]}"}`

func TestReviewSuccess(t *testing.T) {
	var path, auth, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, cannedOK)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()})
	res, err := c.Review(context.Background(), domain.ReviewRequest{
		RepoOwner: "acme",
		RepoName:  "widgets",
		PRNumber:  42,
		PRTitle:   "Add frob",
		HeadSHA:   "abc123",
		Diff:      "diff --git a/x.go b/x.go\n+func F() {}",
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Status != domain.StatusNoFindings || res.Summary != "ok" || len(res.Findings) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if path != "/responses" {
		t.Errorf("path = %q, want /responses", path)
	}
	if auth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", auth)
	}
	if !strings.Contains(body, "json_schema") {
		t.Errorf("request body missing json_schema")
	}
	if !strings.Contains(body, `"strict":true`) {
		t.Errorf("request body missing strict:true")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if parsed["model"] != "deepseek-v4-flash" {
		t.Errorf("model = %v, want deepseek-v4-flash", parsed["model"])
	}
	instructions, _ := parsed["instructions"].(string)
	if !strings.Contains(instructions, "UNTRUSTED DATA") {
		t.Errorf("instructions missing UNTRUSTED DATA warning")
	}
}

func TestValidateResult(t *testing.T) {
	r := domain.ReviewResult{
		Status: domain.StatusChangesRequested,
		Findings: []domain.Finding{
			{
				ID: "ok", File: "x.go", Line: 3,
				Severity: domain.SeverityCritical, Category: domain.CategoryBug,
				Title: "Fix it", Body: "Because it is wrong.",
				Suggestion: &domain.Suggestion{Old: "a()", New: "b()"},
				Confidence: 0.9, Actionable: true,
			},
			{
				ID: "bad-severity", File: "x.go",
				Severity: domain.Severity("catastrophic"), Category: domain.CategoryBug,
				Title: "t", Body: "b", Confidence: 0.5, Actionable: true,
			},
			{
				ID: "not-actionable", File: "x.go",
				Severity: domain.SeverityLow, Category: domain.CategoryStyle,
				Title: "t", Body: "b", Confidence: 0.5, Actionable: false,
			},
			{
				ID: "empty-title", File: "x.go",
				Severity: domain.SeverityMedium, Category: domain.CategoryTest,
				Body: "b", Confidence: 0.5, Actionable: true,
			},
		},
	}
	if err := validateResult(&r); err != nil {
		t.Fatalf("validateResult: %v", err)
	}
	if len(r.Findings) != 1 {
		t.Fatalf("kept %d findings, want 1: %+v", len(r.Findings), r.Findings)
	}
	f := r.Findings[0]
	if f.ID != "ok" {
		t.Errorf("kept finding = %q, want ok", f.ID)
	}
	if f.Suggestion == nil || f.Suggestion.Old != "a()" || f.Suggestion.New != "b()" {
		t.Errorf("suggestion not preserved: %+v", f.Suggestion)
	}
	if err := validateResult(&domain.ReviewResult{Status: domain.ReviewStatus("bogus")}); err == nil {
		t.Error("validateResult accepted invalid status")
	}
}

func TestReviewEmptyOutputThenSuccess(t *testing.T) {
	calls := 0
	var secondInstructions string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			io.WriteString(w, `{"status":"completed","output_text":""}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		json.Unmarshal(b, &parsed)
		secondInstructions, _ = parsed["instructions"].(string)
		io.WriteString(w, cannedOK)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()})
	res, err := c.Review(context.Background(), domain.ReviewRequest{RepoOwner: "a", RepoName: "b"})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Status != domain.StatusNoFindings {
		t.Fatalf("status = %q", res.Status)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if !strings.Contains(secondInstructions, "no_findings") {
		t.Errorf("retry instructions missing no_findings hint")
	}
}

func TestReviewFailedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"failed","error":"model exploded"}`)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()})
	if _, err := c.Review(context.Background(), domain.ReviewRequest{}); err == nil {
		t.Fatal("expected error for failed status")
	}
}

func TestReviewIncompleteStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}`)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()})
	if _, err := c.Review(context.Background(), domain.ReviewRequest{}); err == nil {
		t.Fatal("expected error for incomplete status")
	}
}

func TestReviewRetriesOn429(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":"rate limited"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, cannedOK)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()})
	res, err := c.Review(context.Background(), domain.ReviewRequest{RepoOwner: "a", RepoName: "b"})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if res.Status != domain.StatusNoFindings {
		t.Fatalf("status = %q", res.Status)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}
