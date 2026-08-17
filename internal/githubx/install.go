package githubx

import (
	"context"
	"fmt"

	"github.com/google/go-github/v69/github"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

type installCtxKey struct{}

// WithInstallation binds repo-scoped API calls made in ctx to a specific
// installation ID, so a single Client can serve multiple installations
// concurrently.
func WithInstallation(ctx context.Context, installationID int64) context.Context {
	return context.WithValue(ctx, installCtxKey{}, installationID)
}

// AuthCheck validates the App credentials by listing installations with a
// JWT-authenticated client and returns the number of installations.
func (c *Client) AuthCheck(ctx context.Context) (int, error) {
	jwt, err := c.appJWT()
	if err != nil {
		return 0, err
	}
	appClient := c.newClient().WithAuthToken(jwt)
	installs, _, err := appClient.Apps.ListInstallations(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("githubx: auth check: %w", err)
	}
	return len(installs), nil
}

// ListInstallations returns all installations of the app.
func (c *Client) ListInstallations(ctx context.Context) ([]domain.Installation, error) {
	jwt, err := c.appJWT()
	if err != nil {
		return nil, err
	}
	appClient := c.newClient().WithAuthToken(jwt)
	installs, _, err := appClient.Apps.ListInstallations(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("githubx: list installations: %w", err)
	}
	out := make([]domain.Installation, 0, len(installs))
	for _, i := range installs {
		out = append(out, domain.Installation{
			ID:           i.GetID(),
			AccountID:    i.GetAccount().GetID(),
			AccountLogin: i.GetAccount().GetLogin(),
			AccountType:  i.GetAccount().GetType(),
		})
	}
	return out, nil
}

// AppHookConfig returns the app-level webhook configuration.
func (c *Client) AppHookConfig(ctx context.Context) (url, contentType string, secretSet bool, err error) {
	jwt, jerr := c.appJWT()
	if jerr != nil {
		return "", "", false, jerr
	}
	appClient := c.newClient().WithAuthToken(jwt)
	cfg, _, err := appClient.Apps.GetHookConfig(ctx)
	if err != nil {
		return "", "", false, fmt.Errorf("githubx: hook config: %w", err)
	}
	return cfg.GetURL(), cfg.GetContentType(), cfg.GetSecret() != "", nil
}

// AppInfo returns the app name and slug for the authenticated app.
func (c *Client) AppInfo(ctx context.Context) (name, slug string, err error) {
	jwt, jerr := c.appJWT()
	if jerr != nil {
		return "", "", jerr
	}
	client := c.newClient().WithAuthToken(jwt)
	req, rerr := client.NewRequest("GET", "app", nil)
	if rerr != nil {
		return "", "", rerr
	}
	var info struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if _, err := client.Do(ctx, req, &info); err != nil {
		return "", "", fmt.Errorf("githubx: get app: %w", err)
	}
	return info.Name, info.Slug, nil
}

// SetAppHookURL updates the app-level webhook URL.
func (c *Client) SetAppHookURL(ctx context.Context, url string) error {
	jwt, jerr := c.appJWT()
	if jerr != nil {
		return jerr
	}
	client := c.newClient().WithAuthToken(jwt)
	_, _, err := client.Apps.UpdateHookConfig(ctx, &github.HookConfig{
		URL:         github.String(url),
		ContentType: github.String("json"),
	})
	if err != nil {
		return fmt.Errorf("githubx: update hook config: %w", err)
	}
	return nil
}

// ResolveSelfLogin determines the bot's own login by querying /user with the
// first installation's token, and caches it on the client. Returns the
// configured login when one is set. Returns ("", nil) when the app has no
// installations yet.
func (c *Client) ResolveSelfLogin(ctx context.Context) (string, error) {
	c.selfMu.Lock()
	defer c.selfMu.Unlock()
	if c.cfg.SelfLogin != "" {
		return c.cfg.SelfLogin, nil
	}
	if c.resolvedLogin != "" {
		return c.resolvedLogin, nil
	}
	jwt, err := c.appJWT()
	if err != nil {
		return "", err
	}
	appClient := c.newClient().WithAuthToken(jwt)
	installs, _, err := appClient.Apps.ListInstallations(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("githubx: list installations: %w", err)
	}
	if len(installs) == 0 {
		return "", nil
	}
	client, err := c.clientFor(ctx, installs[0].GetID())
	if err != nil {
		return "", err
	}
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return "", fmt.Errorf("githubx: resolve self login: %w", err)
	}
	c.resolvedLogin = user.GetLogin()
	c.cfg.SelfLogin = c.resolvedLogin
	return c.resolvedLogin, nil
}
