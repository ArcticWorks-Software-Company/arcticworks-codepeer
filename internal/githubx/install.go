package githubx

import (
	"context"
	"fmt"
)

type installCtxKey struct{}

// WithInstallation binds repo-scoped API calls made in ctx to a specific
// installation ID, so a single Client can serve multiple installations
// concurrently.
func WithInstallation(ctx context.Context, installationID int64) context.Context {
	return context.WithValue(ctx, installCtxKey{}, installationID)
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
