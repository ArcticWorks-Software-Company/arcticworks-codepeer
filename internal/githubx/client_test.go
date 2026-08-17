package githubx

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

func testPrivateKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server, *atomic.Int64) {
	t.Helper()
	var tokenCalls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/1/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"token":"inst-token-123","expires_at":"2099-01-01T00:00:00Z"}`)
	})
	mux.Handle("/", handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	c, err := New(Config{
		AppID:      123456,
		ClientID:   "Iv1.abcdef",
		PrivateKey: testPrivateKey(t),
		SelfLogin:  "codepeer-bot[bot]",
		BaseURL:    server.URL + "/",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstallation(1)
	return c, server, &tokenCalls
}

func TestVerifySignature(t *testing.T) {
	secret := []byte("webhook-secret")
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifySignature(secret, body, header) {
		t.Fatal("expected valid signature to verify")
	}
	if VerifySignature([]byte("wrong"), body, header) {
		t.Fatal("expected wrong secret to fail")
	}
	if VerifySignature(secret, []byte(`{"action":"closed"}`), header) {
		t.Fatal("expected tampered body to fail")
	}
	if VerifySignature(secret, body, "") {
		t.Fatal("expected empty header to fail")
	}
	if VerifySignature(secret, body, "sha1="+hex.EncodeToString(mac.Sum(nil))) {
		t.Fatal("expected wrong prefix to fail")
	}
	if VerifySignature(secret, body, "sha256=zzzz") {
		t.Fatal("expected malformed hex to fail")
	}
}

func TestInstallationTokenCaches(t *testing.T) {
	c, _, tokenCalls := newTestClient(t, http.NotFoundHandler())
	first, err := c.InstallationToken(context.Background(), 1)
	if err != nil {
		t.Fatalf("InstallationToken: %v", err)
	}
	second, err := c.InstallationToken(context.Background(), 1)
	if err != nil {
		t.Fatalf("InstallationToken: %v", err)
	}
	if first != "inst-token-123" || first != second {
		t.Fatalf("token mismatch: %q %q", first, second)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("server hits = %d, want 1", got)
	}
}

func TestInstallationTokenRefreshesWhenExpiring(t *testing.T) {
	var tokenCalls atomic.Int64
	expiry := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/1/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		n := tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":"tok-%d","expires_at":%q}`, n, expiry)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	c, err := New(Config{AppID: 1, PrivateKey: testPrivateKey(t), BaseURL: server.URL + "/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first, err := c.InstallationToken(context.Background(), 1)
	if err != nil {
		t.Fatalf("InstallationToken: %v", err)
	}
	second, err := c.InstallationToken(context.Background(), 1)
	if err != nil {
		t.Fatalf("InstallationToken: %v", err)
	}
	if first == second {
		t.Fatal("expected a fresh token for an expiring one")
	}
	if got := tokenCalls.Load(); got != 2 {
		t.Fatalf("server hits = %d, want 2", got)
	}
}

func TestGetRawDiff(t *testing.T) {
	const diff = "diff --git a/main.go b/main.go\nindex 000..111 100644\n--- a/main.go\n+++ b/main.go\n@@ -1 +1,2 @@\n+package main\n"
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/7" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
		fmt.Fprint(w, diff)
	}))
	got, err := c.GetRawDiff(context.Background(), "o", "r", 7)
	if err != nil {
		t.Fatalf("GetRawDiff: %v", err)
	}
	if got != diff {
		t.Fatalf("diff = %q, want %q", got, diff)
	}
}

func TestCreateReviewEventComment(t *testing.T) {
	var mu sync.Mutex
	var body []byte
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/7/reviews" {
			http.NotFound(w, r)
			return
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		mu.Lock()
		body = b
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":42}`)
	}))
	id, err := c.CreateReview(context.Background(), "o", "r", 7, "abc123", "lgtm", []domain.InlineComment{
		{Path: "main.go", Body: "nit", Line: 5, Side: "RIGHT"},
		{Path: "main.go", Body: "multi", Line: 9, Side: "RIGHT", StartLine: 7, StartSide: "RIGHT"},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d, want 42", id)
	}
	mu.Lock()
	defer mu.Unlock()
	var payload struct {
		Event    string `json:"event"`
		CommitID string `json:"commit_id"`
		Comments []struct {
			Path      string `json:"path"`
			Line      int    `json:"line"`
			Side      string `json:"side"`
			StartLine int    `json:"start_line"`
			StartSide string `json:"start_side"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if payload.Event != "COMMENT" {
		t.Fatalf("event = %q, want COMMENT", payload.Event)
	}
	if payload.CommitID != "abc123" {
		t.Fatalf("commit_id = %q, want abc123", payload.CommitID)
	}
	if len(payload.Comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(payload.Comments))
	}
	if cm := payload.Comments[0]; cm.Path != "main.go" || cm.Line != 5 || cm.Side != "RIGHT" || cm.StartLine != 0 {
		t.Fatalf("comment = %+v", cm)
	}
	if cm := payload.Comments[1]; cm.Line != 9 || cm.StartLine != 7 || cm.StartSide != "RIGHT" {
		t.Fatalf("multi-line comment = %+v", cm)
	}
}

func TestGetPushDiffMapsFiles(t *testing.T) {
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/compare/aaa...bbb" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ahead","files":[{"filename":"a.go","status":"modified","additions":2,"deletions":1,"patch":"@@ -1 +1,2 @@\n x"}]}`)
	}))
	files, err := c.GetPushDiff(context.Background(), "o", "r", "aaa", "bbb")
	if err != nil {
		t.Fatalf("GetPushDiff: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	f := files[0]
	if f.Path != "a.go" || f.Status != "modified" || f.Additions != 2 || f.Deletions != 1 {
		t.Fatalf("file = %+v", f)
	}
}

func TestGetPushDiffZeroBefore(t *testing.T) {
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/commits/bbb" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sha":"bbb","files":[{"filename":"n.go","status":"added","additions":9,"deletions":0}]}`)
	}))
	files, err := c.GetPushDiff(context.Background(), "o", "r", "0000000000000000000000000000000000000000", "bbb")
	if err != nil {
		t.Fatalf("GetPushDiff: %v", err)
	}
	if len(files) != 1 || files[0].Path != "n.go" || files[0].Additions != 9 {
		t.Fatalf("files = %+v", files)
	}
}

func TestGetPushDiffSameSHA(t *testing.T) {
	c, _, _ := newTestClient(t, http.NotFoundHandler())
	files, err := c.GetPushDiff(context.Background(), "o", "r", "abc", "abc")
	if err != nil {
		t.Fatalf("GetPushDiff: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %+v, want empty", files)
	}
}

func TestGetFileNotFound(t *testing.T) {
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}))
	got, err := c.GetFile(context.Background(), "o", "r", "missing.go", "main")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
}
