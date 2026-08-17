package githubx

import (
	"context"
	"fmt"

	"github.com/google/go-github/v69/github"

	"github.com/ArcticWorks-Software-Company/arcticworks-codepeer/internal/domain"
)

// GetBranchSHA returns the tip SHA of a branch.
func (c *Client) GetBranchSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", err
	}
	var sha string
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		ref, resp, callErr := client.Git.GetRef(ctx, owner, repo, "heads/"+branch)
		if ref != nil {
			sha = ref.GetObject().GetSHA()
		}
		return resp, callErr
	})
	if err != nil {
		return "", fmt.Errorf("githubx: get branch %s: %w", branch, err)
	}
	if sha == "" {
		return "", fmt.Errorf("githubx: branch %s has no sha", branch)
	}
	return sha, nil
}

// GetCommitTreeSHA returns the tree SHA of a commit.
func (c *Client) GetCommitTreeSHA(ctx context.Context, owner, repo, commitSHA string) (string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", err
	}
	var sha string
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		commit, resp, callErr := client.Git.GetCommit(ctx, owner, repo, commitSHA)
		if commit != nil {
			sha = commit.GetTree().GetSHA()
		}
		return resp, callErr
	})
	if err != nil {
		return "", fmt.Errorf("githubx: get commit %s: %w", commitSHA, err)
	}
	return sha, nil
}

// GetFileWithSHA returns file content and its blob SHA at a ref.
func (c *Client) GetFileWithSHA(ctx context.Context, owner, repo, path, ref string) (string, string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", "", err
	}
	var content, sha string
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		f, _, resp, callErr := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
		if f != nil {
			content, _ = f.GetContent()
			sha = f.GetSHA()
		}
		return resp, callErr
	})
	if err != nil {
		return "", "", fmt.Errorf("githubx: get file %s: %w", path, err)
	}
	return content, sha, nil
}

// CreateBlob creates a git blob and returns its SHA.
func (c *Client) CreateBlob(ctx context.Context, owner, repo, content string) (string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", err
	}
	var sha string
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		blob, resp, callErr := client.Git.CreateBlob(ctx, owner, repo, &github.Blob{Content: github.String(content)})
		if blob != nil {
			sha = blob.GetSHA()
		}
		return resp, callErr
	})
	if err != nil {
		return "", fmt.Errorf("githubx: create blob: %w", err)
	}
	return sha, nil
}

// CreateTree creates a git tree based on baseTreeSHA with the given entries.
func (c *Client) CreateTree(ctx context.Context, owner, repo, baseTreeSHA string, entries []domain.TreeEntry) (string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", err
	}
	ghEntries := make([]*github.TreeEntry, 0, len(entries))
	for _, e := range entries {
		ghEntries = append(ghEntries, &github.TreeEntry{
			Path: github.String(e.Path),
			Mode: github.String(e.Mode),
			Type: github.String(e.Type),
			SHA:  github.String(e.SHA),
		})
	}
	var sha string
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		tree, resp, callErr := client.Git.CreateTree(ctx, owner, repo, baseTreeSHA, ghEntries)
		if tree != nil {
			sha = tree.GetSHA()
		}
		return resp, callErr
	})
	if err != nil {
		return "", fmt.Errorf("githubx: create tree: %w", err)
	}
	return sha, nil
}

// CreateCommit creates a commit on top of parents and returns its SHA.
func (c *Client) CreateCommit(ctx context.Context, owner, repo, message, treeSHA string, parents []string) (string, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return "", err
	}
	ghParents := make([]*github.Commit, 0, len(parents))
	for _, p := range parents {
		ghParents = append(ghParents, &github.Commit{SHA: github.String(p)})
	}
	var sha string
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		commit, resp, callErr := client.Git.CreateCommit(ctx, owner, repo, &github.Commit{
			Message: github.String(message),
			Tree:    &github.Tree{SHA: github.String(treeSHA)},
			Parents: ghParents,
		}, nil)
		if commit != nil {
			sha = commit.GetSHA()
		}
		return resp, callErr
	})
	if err != nil {
		return "", fmt.Errorf("githubx: create commit: %w", err)
	}
	return sha, nil
}

// CreateBranch creates a branch ref pointing at sha. Returns nil if the
// branch already exists.
func (c *Client) CreateBranch(ctx context.Context, owner, repo, name, sha string) error {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return err
	}
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		_, resp, callErr := client.Git.CreateRef(ctx, owner, repo, &github.Reference{
			Ref:    github.String("refs/heads/" + name),
			Object: &github.GitObject{SHA: github.String(sha)},
		})
		return resp, callErr
	})
	if err != nil {
		if respErr, ok := err.(*github.ErrorResponse); ok && respErr.Response != nil && respErr.Response.StatusCode == 422 {
			return nil
		}
		return fmt.Errorf("githubx: create branch %s: %w", name, err)
	}
	return nil
}

// CreatePR opens a pull request and returns its number.
func (c *Client) CreatePR(ctx context.Context, owner, repo, title, body, head, base string) (int, error) {
	client, err := c.clientFor(ctx, c.installationID.Load())
	if err != nil {
		return 0, err
	}
	var number int
	_, err = c.doWithRetry(ctx, func() (*github.Response, error) {
		pr, resp, callErr := client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
			Title: github.String(title),
			Body:  github.String(body),
			Head:  github.String(head),
			Base:  github.String(base),
		})
		if pr != nil {
			number = pr.GetNumber()
		}
		return resp, callErr
	})
	if err != nil {
		return 0, fmt.Errorf("githubx: create pr: %w", err)
	}
	return number, nil
}
