// Package githubx implements GitHub App authentication, webhook signature
// verification, and GitHub REST API wrappers for the CodePeer review bot.
package githubx

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v69/github"
)

const (
	jwtValidity        = 10 * time.Minute
	jwtRefreshWindow   = 2 * time.Minute
	installTokenBuffer = 5 * time.Minute
	installTokenTTL    = time.Hour
	defaultBaseURL     = "https://api.github.com/"
)

// Config holds the GitHub App identity used by Client.
type Config struct {
	AppID      int64
	ClientID   string
	PrivateKey []byte
	SelfLogin  string
	BaseURL    string
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// Client is a GitHub App client. It implements domain.GitHubAPI.
type Client struct {
	cfg        Config
	key        *rsa.PrivateKey
	httpClient *http.Client
	baseURL    *url.URL

	installationID atomic.Int64

	jwtMu     sync.Mutex
	jwtToken  string
	jwtExpiry time.Time

	tokenMu sync.Mutex
	tokens  map[int64]*cachedToken

	selfMu        sync.Mutex
	resolvedLogin string
}

// New parses the app private key and returns a ready Client.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("githubx: parse base url: %w", err)
	}
	block, _ := pem.Decode(cfg.PrivateKey)
	if block == nil {
		return nil, fmt.Errorf("githubx: invalid PEM private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if pkcs8Err != nil {
			return nil, fmt.Errorf("githubx: parse private key: %w", err)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("githubx: private key is not RSA")
		}
		key = rsaKey
	}
	return &Client{
		cfg:        cfg,
		key:        key,
		httpClient: http.DefaultClient,
		baseURL:    base,
		tokens:     make(map[int64]*cachedToken),
	}, nil
}

// SetHTTPClient overrides the HTTP client used for API calls.
func (c *Client) SetHTTPClient(hc *http.Client) {
	if hc != nil {
		c.httpClient = hc
	}
}

// SetInstallation binds the repo-scoped API methods to an installation ID.
func (c *Client) SetInstallation(installationID int64) {
	c.installationID.Store(installationID)
}

func (c *Client) appJWT() (string, error) {
	c.jwtMu.Lock()
	defer c.jwtMu.Unlock()
	if c.jwtToken != "" && time.Until(c.jwtExpiry) > jwtRefreshWindow {
		return c.jwtToken, nil
	}
	now := time.Now()
	issuer := c.cfg.ClientID
	if issuer == "" {
		issuer = strconv.FormatInt(c.cfg.AppID, 10)
	}
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtValidity)),
		Issuer:    issuer,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("githubx: sign app jwt: %w", err)
	}
	c.jwtToken = signed
	c.jwtExpiry = now.Add(jwtValidity)
	return signed, nil
}

// InstallationToken returns a cached installation access token.
func (c *Client) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	c.tokenMu.Lock()
	tok := c.tokens[installationID]
	if tok != nil && time.Until(tok.expiresAt) > installTokenBuffer {
		c.tokenMu.Unlock()
		return tok.token, nil
	}
	c.tokenMu.Unlock()

	jwt, err := c.appJWT()
	if err != nil {
		return "", err
	}
	appClient := c.newClient().WithAuthToken(jwt)

	var created *github.InstallationToken
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		t, resp, err := appClient.Apps.CreateInstallationToken(ctx, installationID, &github.InstallationTokenOptions{})
		created = t
		return resp, err
	})
	if err != nil {
		return "", fmt.Errorf("githubx: create installation token: %w", err)
	}
	token := created.GetToken()
	if token == "" {
		return "", fmt.Errorf("githubx: empty installation token")
	}
	expiry := time.Now().Add(installTokenTTL)
	if e := created.GetExpiresAt(); !e.IsZero() {
		expiry = e.Time
	}
	c.tokenMu.Lock()
	c.tokens[installationID] = &cachedToken{token: token, expiresAt: expiry}
	c.tokenMu.Unlock()
	return token, nil
}

func (c *Client) clientFor(ctx context.Context, installationID int64) (*github.Client, error) {
	if v, ok := ctx.Value(installCtxKey{}).(int64); ok {
		installationID = v
	}
	token, err := c.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	return c.newClient().WithAuthToken(token), nil
}

func (c *Client) newClient() *github.Client {
	gh := github.NewClient(c.httpClient)
	if c.baseURL != nil {
		gh.BaseURL = c.baseURL
	}
	return gh
}
