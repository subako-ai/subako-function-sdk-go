// A Subako function package that reads GitHub: a repository's stats, its open
// issues, and an owner's avatar. Requests go through the worker, which
// attaches a GitHub token when the agent's grant holds one.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	subakofn "github.com/subako-ai/subako-function-sdk-go"
)

const api = "https://api.github.com"

// getJSON decodes the GitHub API's answer at path into into, or says what
// went wrong in words the model can act on.
func getJSON(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return fmt.Errorf("GitHub has nothing at %s", path)
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("GitHub answered %s for %s", res.Status, path)
	}
	return json.NewDecoder(res.Body).Decode(into)
}

type RepoInput struct {
	Owner string `json:"owner" jsonschema:"the user or organization, e.g. golang"`
	Repo  string `json:"repo" jsonschema:"the repository name, e.g. go"`
}

type Repo struct {
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	Stars         int    `json:"stargazers_count"`
	Forks         int    `json:"forks_count"`
	OpenIssues    int    `json:"open_issues_count"`
	DefaultBranch string `json:"default_branch"`
	URL           string `json:"html_url"`
}

func repo(ctx context.Context, req *mcp.CallToolRequest, in RepoInput) (*mcp.CallToolResult, Repo, error) {
	var found Repo
	err := getJSON(ctx, "/repos/"+url.PathEscape(in.Owner)+"/"+url.PathEscape(in.Repo), &found)
	return nil, found, err
}

type IssuesInput struct {
	Owner string `json:"owner" jsonschema:"the user or organization"`
	Repo  string `json:"repo" jsonschema:"the repository name"`
	Label string `json:"label,omitempty" jsonschema:"only issues with this label"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many to return, 1 to 50; 10 when omitted"`
}

type Issue struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	Comments int    `json:"comments"`
	URL      string `json:"url"`
}

type Issues struct {
	Issues []Issue `json:"issues"`
}

func issues(ctx context.Context, req *mcp.CallToolRequest, in IssuesInput) (*mcp.CallToolResult, Issues, error) {
	limit := in.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 50 {
		return nil, Issues{}, fmt.Errorf("limit must be between 1 and 50, not %d", limit)
	}
	query := url.Values{"state": {"open"}, "per_page": {strconv.Itoa(limit)}}
	if in.Label != "" {
		query.Set("labels", in.Label)
	}
	var listed []struct {
		Number      int                    `json:"number"`
		Title       string                 `json:"title"`
		User        struct{ Login string } `json:"user"`
		Comments    int                    `json:"comments"`
		URL         string                 `json:"html_url"`
		PullRequest *struct{}              `json:"pull_request"`
	}
	path := "/repos/" + url.PathEscape(in.Owner) + "/" + url.PathEscape(in.Repo) + "/issues?" + query.Encode()
	if err := getJSON(ctx, path, &listed); err != nil {
		return nil, Issues{}, err
	}
	found := Issues{Issues: []Issue{}}
	for _, issue := range listed {
		// The issues API lists pull requests too.
		if issue.PullRequest == nil {
			found.Issues = append(found.Issues, Issue{issue.Number, issue.Title, issue.User.Login, issue.Comments, issue.URL})
		}
	}
	return nil, found, nil
}

type AvatarInput struct {
	Owner string `json:"owner" jsonschema:"the user or organization"`
}

type Avatar struct {
	Path  string `json:"path"`
	Bytes uint64 `json:"bytes"`
}

// avatar saves the image to the volume rather than reading it back, since
// net/http carries text bodies only.
func avatar(ctx context.Context, req *mcp.CallToolRequest, in AvatarInput) (*mcp.CallToolResult, Avatar, error) {
	path := "/volume/tmp/avatar-" + url.PathEscape(in.Owner) + ".png"
	res, err := subakofn.Do(subakofn.Request{
		URL:    "https://github.com/" + url.PathEscape(in.Owner) + ".png?size=200",
		Output: subakofn.ToFile(path),
	})
	if err != nil {
		return nil, Avatar{}, err
	}
	if res.Status != http.StatusOK {
		return nil, Avatar{}, fmt.Errorf("GitHub answered %d for %s's avatar", res.Status, in.Owner)
	}
	saved := Avatar{Path: path, Bytes: res.Body.Bytes}
	shown, err := subakofn.ImageResult(saved, path)
	return shown, saved, err
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "github", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "repo", Description: "A GitHub repository's description, stars, forks and open issue count."}, repo)
	mcp.AddTool(server, &mcp.Tool{Name: "issues", Description: "A GitHub repository's open issues, newest first, optionally only those with a label."}, issues)
	mcp.AddTool(server, &mcp.Tool{Name: "avatar", Description: "Show a GitHub user's or organization's avatar."}, avatar)
	subakofn.Serve(server)
}
