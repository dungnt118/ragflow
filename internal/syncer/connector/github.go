//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGitHubBatchSize    = 32
	githubItemsPerPage        = 100
	githubRequestTimeout      = 60 * time.Second
	maxGitHubFileBytes        = 20 * 1024 * 1024
	githubSyncScopeRepository = "repository"
	githubSyncScopeDirectory  = "directory"
)

var githubExcludePatterns = []string{
	".git",
	".github",
	"node_modules",
	"vendor",
	"dist",
	"build",
	"__pycache__",
	".next",
	".venv",
	"venv",
	"target",
}

// GitHubConnector reads GitHub repository files, issues, and pull requests.
type GitHubConnector struct {
	owner            string
	repos            []string
	includePRs       bool
	includeIssues    bool
	includeCodeFiles bool
	branch           string
	syncScope        string
	folderPath       string
	rawFolderPath    string
	token            string
	batchSize        int
	baseURL          string
	webBaseURL       string
	doJSON           func(ctx context.Context, apiURL string, out any) (http.Header, error)
	doRaw            func(ctx context.Context, apiURL string) ([]byte, error)
}

// NewGitHubConnector creates a GitHub connector from Python-compatible config.
func NewGitHubConnector(config map[string]any) (*GitHubConnector, error) {
	credentials, _ := config["credentials"].(map[string]any)
	token, _ := credentials["github_access_token"].(string)
	baseURL := strings.TrimRight(os.Getenv("GITHUB_CONNECTOR_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	webBaseURL := strings.TrimRight(os.Getenv("GITHUB_WEB_BASE_URL"), "/")
	if webBaseURL == "" {
		webBaseURL = "https://github.com"
	}
	rawFolderPath := stringConfig(config["folder_path"])
	folderPath, _ := normalizeGitHubFolderPath(rawFolderPath)
	syncScope := strings.ToLower(strings.TrimSpace(stringConfig(config["sync_scope"])))
	if syncScope == "" {
		if folderPath != "" {
			syncScope = githubSyncScopeDirectory
		} else {
			syncScope = githubSyncScopeRepository
		}
	}
	return &GitHubConnector{
		owner:            strings.TrimSpace(stringConfig(config["repository_owner"])),
		repos:            splitGitHubRepos(stringConfig(config["repository_name"])),
		includePRs:       configBoolDefault(config["include_pull_requests"], true),
		includeIssues:    configBoolDefault(config["include_issues"], true),
		includeCodeFiles: configBoolDefault(config["include_code_files"], false),
		branch:           strings.TrimSpace(stringConfig(config["branch"])),
		syncScope:        syncScope,
		folderPath:       folderPath,
		rawFolderPath:    rawFolderPath,
		token:            strings.TrimSpace(token),
		batchSize:        configInt(config["batch_size"], defaultGitHubBatchSize),
		baseURL:          baseURL,
		webBaseURL:       webBaseURL,
	}, nil
}

// withRequestOverrides returns a shallow copy of GitHubConnector with unsaved request overrides applied.
func (c *GitHubConnector) withRequestOverrides(request map[string]any) *GitHubConnector {
	if len(request) == 0 {
		return c
	}
	clone := *c
	cfg := request
	if nested, ok := request["config"].(map[string]any); ok {
		cfg = nested
	}
	if v, ok := cfg["repository_owner"]; ok {
		clone.owner = strings.TrimSpace(stringConfig(v))
	}
	if v, ok := cfg["repository_name"]; ok {
		clone.repos = splitGitHubRepos(stringConfig(v))
	}
	if v, ok := cfg["include_pull_requests"]; ok {
		clone.includePRs = configBoolDefault(v, clone.includePRs)
	}
	if v, ok := cfg["include_issues"]; ok {
		clone.includeIssues = configBoolDefault(v, clone.includeIssues)
	}
	if v, ok := cfg["include_code_files"]; ok {
		clone.includeCodeFiles = configBoolDefault(v, clone.includeCodeFiles)
	}
	if v, ok := cfg["branch"]; ok {
		clone.branch = strings.TrimSpace(stringConfig(v))
	}
	if v, ok := cfg["folder_path"]; ok {
		clone.rawFolderPath = stringConfig(v)
		clone.folderPath, _ = normalizeGitHubFolderPath(clone.rawFolderPath)
	}
	if v, ok := cfg["sync_scope"]; ok {
		scope := strings.ToLower(strings.TrimSpace(stringConfig(v)))
		if scope != "" {
			clone.syncScope = scope
		}
	}
	if creds, ok := request["credentials"].(map[string]any); ok {
		if tok, hasTok := creds["github_access_token"].(string); hasTok && strings.TrimSpace(tok) != "" {
			clone.token = strings.TrimSpace(tok)
		}
	}
	return &clone
}

// normalizeGitHubFolderPath normalizes a repository-relative folder path and rejects path traversal.
func normalizeGitHubFolderPath(raw string) (string, error) {
	cleaned := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" {
		return "", nil
	}
	parts := strings.Split(cleaned, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			return "", fmt.Errorf("Invalid connector settings: 'folder_path' must not contain '.' or '..'")
		}
		out = append(out, part)
	}
	return strings.Join(out, "/"), nil
}

// effectiveFolderPath returns the active folder filter when syncing a specific directory.
func (c *GitHubConnector) effectiveFolderPath() string {
	if c.syncScope == githubSyncScopeDirectory {
		return c.folderPath
	}
	return ""
}

// effectiveBranch resolves the target branch for a repository.
func (c *GitHubConnector) effectiveBranch(repo githubRepo) string {
	if strings.TrimSpace(c.branch) != "" {
		return strings.TrimSpace(c.branch)
	}
	if strings.TrimSpace(repo.DefaultBranch) != "" {
		return strings.TrimSpace(repo.DefaultBranch)
	}
	return "main"
}

// initialStage returns the first sync/prune stage based on enabled connector options.
func (c *GitHubConnector) initialStage() string {
	if c.includeCodeFiles {
		return githubStageCodeFiles
	}
	return githubStagePRs
}

// Validate validates GitHub connector settings and credentials.
func (c *GitHubConnector) Validate(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("github connector is nil")
	}
	if c.owner == "" {
		return fmt.Errorf("Invalid connector settings: 'repository_owner' must be provided")
	}
	if c.token == "" {
		return fmt.Errorf("Missing github_access_token in credentials")
	}
	if c.batchSize <= 0 {
		return fmt.Errorf("batch_size must be a positive integer")
	}
	if !c.includePRs && !c.includeIssues && !c.includeCodeFiles {
		return fmt.Errorf("Invalid connector settings: at least one of 'include_pull_requests', 'include_issues', or 'include_code_files' must be enabled")
	}
	if c.includeCodeFiles {
		if _, err := normalizeGitHubFolderPath(c.rawFolderPath); err != nil {
			return err
		}
		if c.syncScope != githubSyncScopeRepository && c.syncScope != githubSyncScopeDirectory {
			return fmt.Errorf("Invalid connector settings: 'sync_scope' must be 'repository' or 'directory'")
		}
		if c.syncScope == githubSyncScopeDirectory && c.folderPath == "" {
			return fmt.Errorf("Invalid connector settings: 'folder_path' must be provided when 'sync_scope' is 'directory'")
		}
	}
	repos, err := c.listRepos(ctx)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return fmt.Errorf("found no repos for GitHub owner %s", c.owner)
	}
	return nil
}

// ValidateConnectorSetting validates GitHub settings from an unsaved config.
func (c *GitHubConnector) ValidateConnectorSetting(ctx context.Context, request map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, connectorSettingValidationTimeout)
	defer cancel()
	if c == nil {
		return &ConnectorValidationError{Message: "github connector is nil"}
	}
	c = c.withRequestOverrides(request)
	if c.token == "" {
		return &ConnectorMissingCredentialError{Message: "Missing github_access_token in credentials"}
	}
	if c.owner == "" {
		return &ConnectorValidationError{Message: "Invalid connector settings: 'repo_owner' must be provided."}
	}
	if c.batchSize <= 0 {
		return &ConnectorValidationError{Message: "batch_size must be a positive integer"}
	}
	if !c.includePRs && !c.includeIssues && !c.includeCodeFiles {
		return &ConnectorValidationError{Message: "Invalid connector settings: at least one of 'include_pull_requests', 'include_issues', or 'include_code_files' must be enabled."}
	}
	if c.includeCodeFiles {
		if _, err := normalizeGitHubFolderPath(c.rawFolderPath); err != nil {
			return &ConnectorValidationError{Message: err.Error()}
		}
		if c.syncScope != githubSyncScopeRepository && c.syncScope != githubSyncScopeDirectory {
			return &ConnectorValidationError{Message: "Invalid connector settings: 'sync_scope' must be 'repository' or 'directory'."}
		}
		if c.syncScope == githubSyncScopeDirectory && c.folderPath == "" {
			return &ConnectorValidationError{Message: "Invalid connector settings: 'folder_path' must be provided when 'sync_scope' is 'directory'."}
		}
	}

	if err := c.validateOwner(ctx); err != nil {
		return err
	}
	if c.includeCodeFiles && len(c.repos) > 0 {
		if err := c.validateCodeFilesTargets(ctx); err != nil {
			return err
		}
	}
	return nil
}

// validateCodeFilesTargets verifies that configured repositories, branches, and optional folder paths exist on GitHub.
func (c *GitHubConnector) validateCodeFilesTargets(ctx context.Context) error {
	folderPrefix := c.effectiveFolderPath()
	for _, repoName := range c.repos {
		var repo githubRepo
		repoPath := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repoName)
		if _, err := c.getJSON(ctx, c.apiURL(repoPath, nil), &repo); err != nil {
			var apiErr *githubAPIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				return &ConnectorValidationError{Message: fmt.Sprintf("GitHub repository not found: %s/%s", c.owner, repoName)}
			}
			return classifyGitHubError(err)
		}
		fullName := repo.FullName
		if fullName == "" {
			fullName = c.owner + "/" + repoName
		}
		branch := c.effectiveBranch(repo)
		treeResp, err := c.fetchRepoTree(ctx, fullName, branch)
		if err != nil {
			var apiErr *githubAPIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				return &ConnectorValidationError{Message: fmt.Sprintf("Branch '%s' not found in repository %s", branch, fullName)}
			}
			return classifyGitHubError(err)
		}
		if folderPrefix != "" {
			foundFolder := false
			for _, entry := range treeResp.Tree {
				if entry.Path == folderPrefix || strings.HasPrefix(entry.Path, folderPrefix+"/") {
					foundFolder = true
					break
				}
			}
			if !foundFolder {
				return &ConnectorValidationError{Message: fmt.Sprintf("Folder path '%s' not found in %s (branch '%s')", folderPrefix, fullName, branch)}
			}
		}
	}
	return nil
}

// validateOwner validates the owner as an organization or a user with repositories.
func (c *GitHubConnector) validateOwner(ctx context.Context) error {
	err := c.validateOrgRepos(ctx)
	if err == nil {
		return nil
	}
	var apiErr *githubAPIError
	if errors.As(err, &apiErr) {
		if isGitHubMissingSSO(apiErr.Message) {
			const ssoGuideLink = "https://docs.github.com/en/enterprise-cloud@latest/authentication/authenticating-with-saml-single-sign-on/authorizing-a-personal-access-token-for-use-with-saml-single-sign-on"
			return &ConnectorValidationError{Message: fmt.Sprintf("Your GitHub token is missing authorization to access the `%s` organization. Please follow the guide to authorize your token: %s", c.owner, ssoGuideLink)}
		}
		if apiErr.Status != http.StatusNotFound {
			return classifyGitHubError(apiErr)
		}
		// The owner is not accessible as an organization; try as a user.
		userErr := c.validateUserRepos(ctx)
		if userErr == nil {
			return nil
		}
		var userAPIErr *githubAPIError
		if errors.As(userErr, &userAPIErr) && userAPIErr.Status == http.StatusNotFound {
			return &ConnectorValidationError{Message: fmt.Sprintf("GitHub user or organization not found: %s", c.owner)}
		}
		return classifyGitHubError(userErr)
	}
	return classifyGitHubError(err)
}

// validateOrgRepos validates the owner as a GitHub organization with repositories.
func (c *GitHubConnector) validateOrgRepos(ctx context.Context) error {
	var org map[string]any
	if _, err := c.getJSON(ctx, c.apiURL("/orgs/"+url.PathEscape(c.owner), nil), &org); err != nil {
		return err
	}
	hasRepos, err := c.ownerHasRepos(ctx, "/orgs/"+url.PathEscape(c.owner)+"/repos")
	if err != nil {
		return err
	}
	if !hasRepos {
		return &ConnectorValidationError{Message: fmt.Sprintf("Found no repos for organization: %s. Does the credential have the right scopes?", c.owner)}
	}
	return nil
}

// validateUserRepos validates the owner as a GitHub user with repositories.
func (c *GitHubConnector) validateUserRepos(ctx context.Context) error {
	var user map[string]any
	if _, err := c.getJSON(ctx, c.apiURL("/users/"+url.PathEscape(c.owner), nil), &user); err != nil {
		return err
	}
	hasRepos, err := c.ownerHasRepos(ctx, "/users/"+url.PathEscape(c.owner)+"/repos")
	if err != nil {
		return err
	}
	if !hasRepos {
		return &ConnectorValidationError{Message: fmt.Sprintf("Found no repos for user: %s. Does the credential have the right scopes?", c.owner)}
	}
	return nil
}

// ownerHasRepos reports whether an org or user owner has at least one visible repository.
func (c *GitHubConnector) ownerHasRepos(ctx context.Context, path string) (bool, error) {
	query := url.Values{"per_page": {"1"}}
	var batch []githubRepo
	if _, err := c.getJSON(ctx, c.apiURL(path, query), &batch); err != nil {
		return false, err
	}
	return len(batch) > 0, nil
}

// classifyGitHubError maps a GitHub API error to a typed connector error.
func classifyGitHubError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *githubAPIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Status {
	case http.StatusUnauthorized:
		return &ConnectorMissingCredentialError{Message: "GitHub credential appears to be invalid or expired (HTTP 401)."}
	case http.StatusForbidden:
		return &ConnectorValidationError{Message: "Your GitHub token does not have sufficient permissions for this repository (HTTP 403)."}
	default:
		return &ConnectorValidationError{Message: fmt.Sprintf("Unexpected GitHub error (status=%d): %s", apiErr.Status, apiErr.Message)}
	}
}

// isGitHubMissingSSO reports whether a GitHub API error body requires organization SSO authorization.
func isGitHubMissingSSO(body string) bool {
	return strings.Contains(strings.ToLower(body), "you must grant your personal access token access to this organization")
}

// OpenSync opens one GitHub sync session.
func (c *GitHubConnector) OpenSync(ctx context.Context, request SyncRequest) (SyncSession, error) {
	repos, err := c.listRepos(ctx)
	if err != nil {
		return nil, err
	}
	session := &githubSyncSession{
		connector:   c,
		repos:       repos,
		batchSize:   c.batchSize,
		stage:       c.initialStage(),
		page:        1,
		windowStart: request.WindowStart,
		windowEnd:   request.WindowEnd,
	}
	if err := session.applyResume(request.Resume); err != nil {
		return nil, err
	}
	return session, nil
}

// OpenPrune opens one complete GitHub prune snapshot session.
func (c *GitHubConnector) OpenPrune(ctx context.Context, request PruneRequest) (PruneSession, error) {
	repos, err := c.listRepos(ctx)
	if err != nil {
		return nil, err
	}
	return &githubPruneSession{
		connector: c,
		repos:     repos,
		batchSize: c.batchSize,
		stage:     c.initialStage(),
		page:      1,
	}, nil
}

// listRepos returns configured repositories or all owner repositories.
func (c *GitHubConnector) listRepos(ctx context.Context) ([]githubRepo, error) {
	if len(c.repos) > 0 {
		repos := make([]githubRepo, 0, len(c.repos))
		for _, repoName := range c.repos {
			var repo githubRepo
			if _, err := c.getJSON(ctx, c.apiURL("/repos/"+url.PathEscape(c.owner)+"/"+url.PathEscape(repoName), nil), &repo); err != nil {
				return nil, err
			}
			repos = append(repos, repo)
		}
		return repos, nil
	}

	repos, err := c.listRepoEndpoint(ctx, "/orgs/"+url.PathEscape(c.owner)+"/repos")
	if err == nil {
		return repos, nil
	}
	return c.listRepoEndpoint(ctx, "/users/"+url.PathEscape(c.owner)+"/repos")
}

// listRepoEndpoint returns all repos from an org or user endpoint.
func (c *GitHubConnector) listRepoEndpoint(ctx context.Context, path string) ([]githubRepo, error) {
	repos := []githubRepo{}
	for page := 1; ; page++ {
		query := url.Values{"per_page": {strconv.Itoa(githubItemsPerPage)}, "page": {strconv.Itoa(page)}}
		var batch []githubRepo
		headers, err := c.getJSON(ctx, c.apiURL(path, query), &batch)
		if err != nil {
			return nil, err
		}
		repos = append(repos, batch...)
		if !hasNextPage(headers) || len(batch) == 0 {
			break
		}
	}
	return repos, nil
}

// fetchRepoTree calls GitHub Git Trees API with recursive=1 for a repository branch/ref.
func (c *GitHubConnector) fetchRepoTree(ctx context.Context, fullName, branch string) (githubTreeResponse, error) {
	query := url.Values{"recursive": {"1"}}
	var treeResp githubTreeResponse
	if _, err := c.getJSON(ctx, c.apiURL("/repos/"+fullName+"/git/trees/"+url.PathEscape(branch), query), &treeResp); err != nil {
		return githubTreeResponse{}, err
	}
	return treeResp, nil
}

// listCodeFiles returns sorted, filtered repository file blobs for the target repository.
func (c *GitHubConnector) listCodeFiles(ctx context.Context, repo githubRepo) ([]githubTreeEntry, string, error) {
	branch := c.effectiveBranch(repo)
	treeResp, err := c.fetchRepoTree(ctx, repo.FullName, branch)
	if err != nil {
		return nil, "", err
	}
	if treeResp.Truncated {
		return nil, "", fmt.Errorf("github repository tree for %s@%s is truncated (>100k entries); aborting to prevent incomplete sync or prune", repo.FullName, branch)
	}
	folderPrefix := c.effectiveFolderPath()
	files := make([]githubTreeEntry, 0, len(treeResp.Tree))
	for _, entry := range treeResp.Tree {
		if entry.Type != "blob" || entry.Mode == "160000" {
			continue
		}
		if folderPrefix != "" && !strings.HasPrefix(entry.Path, folderPrefix+"/") {
			continue
		}
		if shouldExcludeGitHubPath(entry.Path) {
			continue
		}
		if entry.Size < 0 || entry.Size > maxGitHubFileBytes {
			continue
		}
		if !isSupportedGitHubCodeFile(entry.Path) {
			continue
		}
		files = append(files, entry)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, branch, nil
}

// listCodeFilePage returns one deterministic page of GitHub code/docs files for sync.
func (c *GitHubConnector) listCodeFilePage(files []githubTreeEntry, fullName, branch string, page, pageSize int, windowEnd time.Time) ([]githubBufferedDocument, bool) {
	if pageSize <= 0 {
		pageSize = defaultGitHubBatchSize
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= len(files) {
		return nil, true
	}
	end := start + pageSize
	done := false
	if end >= len(files) {
		end = len(files)
		done = true
	}
	updatedAt := windowEnd.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	slice := files[start:end]
	documents := make([]githubBufferedDocument, 0, len(slice))
	for idx, entry := range slice {
		doc := c.buildCodeFileDocument(fullName, branch, entry, updatedAt)
		pageOffset := idx + 1
		documents = append(documents, githubBufferedDocument{
			document:   doc,
			checkpoint: githubSyncCheckpoint(fullName, githubStageCodeFiles, page, pageOffset, doc),
			offset:     pageOffset,
			sourceID:   doc.SourceID,
		})
	}
	return documents, done
}

// listCodeFileSlimPage returns one deterministic page of GitHub code/docs file SourceIDs for prune.
func (c *GitHubConnector) listCodeFileSlimPage(files []githubTreeEntry, fullName, branch string, page, pageSize int) ([]SlimDocument, bool) {
	if pageSize <= 0 {
		pageSize = defaultGitHubBatchSize
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= len(files) {
		return nil, true
	}
	end := start + pageSize
	done := false
	if end >= len(files) {
		end = len(files)
		done = true
	}
	slice := files[start:end]
	documents := make([]SlimDocument, 0, len(slice))
	for _, entry := range slice {
		documents = append(documents, SlimDocument{SourceID: c.fileWebURL(fullName, branch, entry.Path)})
	}
	return documents, done
}

// buildCodeFileDocument constructs a delayed-fetch SourceDocument for a GitHub repository file.
func (c *GitHubConnector) buildCodeFileDocument(fullName, branch string, entry githubTreeEntry, updatedAt time.Time) SourceDocument {
	webURL := c.fileWebURL(fullName, branch, entry.Path)
	fetchRef := githubFetchReference{
		FullName: fullName,
		Branch:   branch,
		Path:     entry.Path,
		SHA:      entry.SHA,
	}
	fetchKey, _ := json.Marshal(fetchRef)
	semanticName := c.codeFileSemanticName(entry.Path)
	ext := githubFileExt(entry.Path)
	return SourceDocument{
		SourceID:           webURL,
		SemanticIdentifier: semanticName,
		Extension:          ext,
		FetchRef:           &FetchReference{Key: string(fetchKey), SizeHint: entry.Size},
		UpdatedAt:          updatedAt,
		SizeBytes:          entry.Size,
		Metadata: map[string]any{
			"object_type": "CodeFile",
			"type":        "CodeFile",
			"repo":        fullName,
			"branch":      branch,
			"path":        entry.Path,
			"sha":         entry.SHA,
			"web_url":     webURL,
		},
		Fingerprint: stableFingerprint(map[string]any{
			"type":   "CodeFile",
			"repo":   fullName,
			"branch": branch,
			"path":   entry.Path,
			"sha":    entry.SHA,
		}),
	}
}

// codeFileSemanticName computes the display name relative to folder_path (DEC-01: OPT-1).
func (c *GitHubConnector) codeFileSemanticName(filePath string) string {
	rel := filePath
	if prefix := c.effectiveFolderPath(); prefix != "" && strings.HasPrefix(filePath, prefix+"/") {
		rel = strings.TrimPrefix(filePath, prefix+"/")
	}
	rel = strings.Trim(rel, "/")
	if rel == "" {
		rel = filepath.Base(filePath)
	}
	return strings.ReplaceAll(rel, "/", "_")
}

// fileWebURL builds the canonical GitHub web URL for a repository file.
func (c *GitHubConnector) fileWebURL(fullName, branch, filePath string) string {
	base := strings.TrimRight(c.webBaseURL, "/")
	if base == "" {
		base = "https://github.com"
	}
	return fmt.Sprintf("%s/%s/blob/%s/%s", base, strings.Trim(fullName, "/"), branch, strings.TrimLeft(filePath, "/"))
}

// fetchBlobRaw downloads raw file bytes from GitHub Git Blobs API with a 20MB safety cap.
func (c *GitHubConnector) fetchBlobRaw(ctx context.Context, fullName, sha string) ([]byte, error) {
	apiURL := c.apiURL("/repos/"+fullName+"/git/blobs/"+url.PathEscape(sha), nil)
	if c.doRaw != nil {
		return c.doRaw(ctx, apiURL)
	}
	resp, err := connectorRequest(ctx, connectorRequestOptions{
		Method:   http.MethodGet,
		RawURL:   apiURL,
		Validate: assertConnectorURLSafeHTTPS,
		Headers: map[string]string{
			"Accept":               "application/vnd.github.raw+json",
			"X-GitHub-Api-Version": "2022-11-28",
			"Authorization":        "Bearer " + c.token,
		},
		Timeout: githubRequestTimeout,
	})
	if err != nil {
		var unsafe *connectorUnsafeURLError
		if errors.As(err, &unsafe) {
			return nil, unsafe.Err
		}
		return nil, fmt.Errorf("failed to fetch GitHub blob: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &githubAPIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGitHubFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxGitHubFileBytes {
		return nil, fmt.Errorf("github blob %s in %s exceeds maximum allowed size (%d bytes)", sha, fullName, maxGitHubFileBytes)
	}
	return data, nil
}

// listPullRequestPage returns one page of GitHub pull requests.
func (c *GitHubConnector) listPullRequestPage(ctx context.Context, fullName string, page, pageSize int, windowStart *time.Time, windowEnd time.Time) ([]githubBufferedDocument, bool, error) {
	var batch []githubPullRequest
	headers, err := c.getJSON(ctx, c.apiURL("/repos/"+fullName+"/pulls", githubListQuery(page, pageSize)), &batch)
	if err != nil {
		return nil, false, err
	}
	documents := make([]githubBufferedDocument, 0, len(batch))
	doneByWindow := false
	pageOffset := 0
	for _, pr := range batch {
		if beforeOrAtWindowStart(pr.UpdatedAt, windowStart) {
			doneByWindow = true
			break
		}
		if afterWindowEnd(pr.UpdatedAt, windowEnd) {
			continue
		}
		doc := pr.toSourceDocument(fullName)
		pageOffset++
		documents = append(documents, githubBufferedDocument{
			document:   doc,
			checkpoint: githubSyncCheckpoint(fullName, githubStagePRs, page, pageOffset, doc),
			offset:     pageOffset,
			sourceID:   doc.SourceID,
		})
	}
	done := doneByWindow || !hasNextPage(headers) || len(batch) == 0
	return documents, done, nil
}

// listIssuePage returns one page of GitHub issues.
func (c *GitHubConnector) listIssuePage(ctx context.Context, fullName string, page, pageSize int, windowStart *time.Time, windowEnd time.Time) ([]githubBufferedDocument, bool, error) {
	var batch []githubIssue
	headers, err := c.getJSON(ctx, c.apiURL("/repos/"+fullName+"/issues", githubListQuery(page, pageSize)), &batch)
	if err != nil {
		return nil, false, err
	}
	documents := make([]githubBufferedDocument, 0, len(batch))
	doneByWindow := false
	pageOffset := 0
	for _, issue := range batch {
		if issue.PullRequest != nil {
			continue
		}
		if beforeOrAtWindowStart(issue.UpdatedAt, windowStart) {
			doneByWindow = true
			break
		}
		if afterWindowEnd(issue.UpdatedAt, windowEnd) {
			continue
		}
		doc := issue.toSourceDocument(fullName)
		pageOffset++
		documents = append(documents, githubBufferedDocument{
			document:   doc,
			checkpoint: githubSyncCheckpoint(fullName, githubStageIssues, page, pageOffset, doc),
			offset:     pageOffset,
			sourceID:   doc.SourceID,
		})
	}
	done := doneByWindow || !hasNextPage(headers) || len(batch) == 0
	return documents, done, nil
}

// listPullRequestSlimPage returns one page of GitHub pull request IDs.
func (c *GitHubConnector) listPullRequestSlimPage(ctx context.Context, fullName string, page, pageSize int) ([]SlimDocument, bool, error) {
	var batch []githubPullRequest
	headers, err := c.getJSON(ctx, c.apiURL("/repos/"+fullName+"/pulls", githubListQuery(page, pageSize)), &batch)
	if err != nil {
		return nil, false, err
	}
	documents := make([]SlimDocument, 0, len(batch))
	for _, pr := range batch {
		documents = append(documents, SlimDocument{SourceID: pr.HTMLURL})
	}
	return documents, !hasNextPage(headers) || len(batch) == 0, nil
}

// listIssueSlimPage returns one page of GitHub issue IDs.
func (c *GitHubConnector) listIssueSlimPage(ctx context.Context, fullName string, page, pageSize int) ([]SlimDocument, bool, error) {
	var batch []githubIssue
	headers, err := c.getJSON(ctx, c.apiURL("/repos/"+fullName+"/issues", githubListQuery(page, pageSize)), &batch)
	if err != nil {
		return nil, false, err
	}
	documents := make([]SlimDocument, 0, len(batch))
	for _, issue := range batch {
		if issue.PullRequest != nil {
			continue
		}
		documents = append(documents, SlimDocument{SourceID: issue.HTMLURL})
	}
	return documents, !hasNextPage(headers) || len(batch) == 0, nil
}

// githubListQuery builds standard GitHub list query parameters.
func githubListQuery(page, pageSize int) url.Values {
	if pageSize <= 0 {
		pageSize = defaultGitHubBatchSize
	}
	return url.Values{
		"state":     {"all"},
		"sort":      {"updated"},
		"direction": {"desc"},
		"per_page":  {strconv.Itoa(pageSize)},
		"page":      {strconv.Itoa(page)},
	}
}

// getJSON fetches a GitHub API response into out.
func (c *GitHubConnector) getJSON(ctx context.Context, apiURL string, out any) (http.Header, error) {
	if c.doJSON != nil {
		return c.doJSON(ctx, apiURL, out)
	}
	resp, err := connectorRequest(ctx, connectorRequestOptions{
		Method:   http.MethodGet,
		RawURL:   apiURL,
		Validate: assertConnectorURLSafeHTTPS,
		Headers: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28",
			"Authorization":        "Bearer " + c.token,
		},
		Timeout: githubRequestTimeout,
	})
	if err != nil {
		var unsafe *connectorUnsafeURLError
		if errors.As(err, &unsafe) {
			return nil, unsafe.Err
		}
		return nil, fmt.Errorf("failed to fetch GitHub API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &githubAPIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	if err = json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, err
	}
	return resp.Header.Clone(), nil
}

// apiURL builds a GitHub API URL.
func (c *GitHubConnector) apiURL(path string, query url.Values) string {
	path = strings.TrimLeft(path, "/")
	u := strings.TrimRight(c.baseURL, "/") + "/" + path
	if len(query) == 0 {
		return u
	}
	return u + "?" + query.Encode()
}

type githubSyncSession struct {
	connector      *GitHubConnector
	repos          []githubRepo
	repoIndex      int
	stage          string
	page           int
	batchSize      int
	windowStart    *time.Time
	windowEnd      time.Time
	buffer         []githubBufferedDocument
	codeFiles      []githubTreeEntry
	codeFilesBranch string
	codeFilesLoaded bool
	resumeRepo     string
	resumeStage    string
	resumePage     int
	resumeOffset   int
	resumeSourceID string
}

// NextBatch returns the next GitHub document batch.
func (s *githubSyncSession) NextBatch(ctx context.Context) (SyncBatch, error) {
	documents := make([]SourceDocument, 0, s.batchSize)
	var checkpoint *SyncCheckpoint
	if len(s.buffer) > 0 {
		n := s.batchSize
		if n > len(s.buffer) {
			n = len(s.buffer)
		}
		for _, buffered := range s.buffer[:n] {
			documents = append(documents, buffered.document)
			checkpoint = buffered.checkpoint
		}
		s.buffer = s.buffer[n:]
	}

	for len(documents) < s.batchSize {
		if s.repoIndex >= len(s.repos) {
			if len(documents) == 0 {
				return SyncBatch{}, io.EOF
			}
			break
		}
		batch, err := s.nextDocumentPage(ctx)
		if err != nil {
			return SyncBatch{}, err
		}
		remaining := s.batchSize - len(documents)
		if len(batch) > remaining {
			for _, buffered := range batch[:remaining] {
				documents = append(documents, buffered.document)
				checkpoint = buffered.checkpoint
			}
			s.buffer = append(s.buffer, batch[remaining:]...)
			break
		}
		for _, buffered := range batch {
			documents = append(documents, buffered.document)
			checkpoint = buffered.checkpoint
		}
	}
	return SyncBatch{Documents: documents, Checkpoint: checkpoint}, nil
}

// Fetch downloads a GitHub file body for a delayed source document.
func (s *githubSyncSession) Fetch(ctx context.Context, ref FetchReference) ([]byte, error) {
	var fetch githubFetchReference
	if err := json.Unmarshal([]byte(ref.Key), &fetch); err != nil {
		return nil, err
	}
	return s.connector.fetchBlobRaw(ctx, fetch.FullName, fetch.SHA)
}

// Close closes the GitHub sync session.
func (s *githubSyncSession) Close() error {
	return nil
}

type githubPruneSession struct {
	connector       *GitHubConnector
	repos           []githubRepo
	repoIndex       int
	stage           string
	page            int
	batchSize       int
	buffer          []SlimDocument
	codeFiles       []githubTreeEntry
	codeFilesBranch string
	codeFilesLoaded bool
}

// NextBatch returns the next GitHub prune snapshot batch.
func (s *githubPruneSession) NextBatch(ctx context.Context) (PruneBatch, error) {
	documents := make([]SlimDocument, 0, s.batchSize)
	if len(s.buffer) > 0 {
		n := s.batchSize
		if n > len(s.buffer) {
			n = len(s.buffer)
		}
		documents = append(documents, s.buffer[:n]...)
		s.buffer = s.buffer[n:]
	}
	for len(documents) < s.batchSize {
		if s.repoIndex >= len(s.repos) {
			if len(documents) == 0 {
				return PruneBatch{}, io.EOF
			}
			break
		}
		batch, err := s.nextSlimPage(ctx)
		if err != nil {
			return PruneBatch{}, err
		}
		remaining := s.batchSize - len(documents)
		if len(batch) > remaining {
			documents = append(documents, batch[:remaining]...)
			s.buffer = append(s.buffer, batch[remaining:]...)
			break
		}
		documents = append(documents, batch...)
	}
	return PruneBatch{Documents: documents}, nil
}

// Close closes the GitHub prune session.
func (s *githubPruneSession) Close() error {
	return nil
}

const (
	githubStageCodeFiles = "code_files"
	githubStagePRs       = "prs"
	githubStageIssues    = "issues"
)

// nextDocumentPage fetches one GitHub API page for sync.
func (s *githubSyncSession) nextDocumentPage(ctx context.Context) ([]githubBufferedDocument, error) {
	repo := s.repos[s.repoIndex]
	switch s.stage {
	case githubStageCodeFiles:
		if !s.connector.includeCodeFiles {
			s.advanceStage()
			return nil, nil
		}
		if !s.codeFilesLoaded {
			files, branch, err := s.connector.listCodeFiles(ctx, repo)
			if err != nil {
				return nil, err
			}
			s.codeFiles = files
			s.codeFilesBranch = branch
			s.codeFilesLoaded = true
		}
		docs, done := s.connector.listCodeFilePage(s.codeFiles, repo.FullName, s.codeFilesBranch, s.page, s.batchSize, s.windowEnd)
		var err error
		docs, err = s.filterResumedDocuments(repo.FullName, githubStageCodeFiles, s.page, docs)
		if err != nil {
			return nil, err
		}
		if done {
			s.advanceStage()
		} else {
			s.page++
		}
		return docs, nil
	case githubStagePRs:
		if !s.connector.includePRs {
			s.advanceStage()
			return nil, nil
		}
		docs, done, err := s.connector.listPullRequestPage(ctx, repo.FullName, s.page, s.batchSize, s.windowStart, s.windowEnd)
		if err != nil {
			return nil, err
		}
		docs, err = s.filterResumedDocuments(repo.FullName, githubStagePRs, s.page, docs)
		if err != nil {
			return nil, err
		}
		if done {
			s.advanceStage()
		} else {
			s.page++
		}
		return docs, nil
	case githubStageIssues:
		if !s.connector.includeIssues {
			s.advanceRepo()
			return nil, nil
		}
		docs, done, err := s.connector.listIssuePage(ctx, repo.FullName, s.page, s.batchSize, s.windowStart, s.windowEnd)
		if err != nil {
			return nil, err
		}
		docs, err = s.filterResumedDocuments(repo.FullName, githubStageIssues, s.page, docs)
		if err != nil {
			return nil, err
		}
		if done {
			s.advanceRepo()
		} else {
			s.page++
		}
		return docs, nil
	default:
		s.advanceRepo()
		return nil, nil
	}
}

// nextSlimPage fetches one GitHub API page for prune.
func (s *githubPruneSession) nextSlimPage(ctx context.Context) ([]SlimDocument, error) {
	repo := s.repos[s.repoIndex]
	switch s.stage {
	case githubStageCodeFiles:
		if !s.connector.includeCodeFiles {
			s.advanceStage()
			return nil, nil
		}
		if !s.codeFilesLoaded {
			files, branch, err := s.connector.listCodeFiles(ctx, repo)
			if err != nil {
				return nil, err
			}
			s.codeFiles = files
			s.codeFilesBranch = branch
			s.codeFilesLoaded = true
		}
		docs, done := s.connector.listCodeFileSlimPage(s.codeFiles, repo.FullName, s.codeFilesBranch, s.page, s.batchSize)
		if done {
			s.advanceStage()
		} else {
			s.page++
		}
		return docs, nil
	case githubStagePRs:
		if !s.connector.includePRs {
			s.advanceStage()
			return nil, nil
		}
		docs, done, err := s.connector.listPullRequestSlimPage(ctx, repo.FullName, s.page, s.batchSize)
		if err != nil {
			return nil, err
		}
		if done {
			s.advanceStage()
		} else {
			s.page++
		}
		return docs, nil
	case githubStageIssues:
		if !s.connector.includeIssues {
			s.advanceRepo()
			return nil, nil
		}
		docs, done, err := s.connector.listIssueSlimPage(ctx, repo.FullName, s.page, s.batchSize)
		if err != nil {
			return nil, err
		}
		if done {
			s.advanceRepo()
		} else {
			s.page++
		}
		return docs, nil
	default:
		s.advanceRepo()
		return nil, nil
	}
}

// advanceStage moves a GitHub session to the next stage within the same repository.
func (s *githubSyncSession) advanceStage() {
	switch s.stage {
	case githubStageCodeFiles:
		s.stage = githubStagePRs
	case githubStagePRs:
		s.stage = githubStageIssues
	default:
		s.advanceRepo()
		return
	}
	s.page = 1
	s.codeFiles = nil
	s.codeFilesBranch = ""
	s.codeFilesLoaded = false
	s.clearResume()
}

// advanceRepo moves a GitHub session to the next repository.
func (s *githubSyncSession) advanceRepo() {
	s.repoIndex++
	s.stage = s.connector.initialStage()
	s.page = 1
	s.codeFiles = nil
	s.codeFilesBranch = ""
	s.codeFilesLoaded = false
	s.clearResume()
}

// applyResume advances a sync session to the last committed GitHub page.
func (s *githubSyncSession) applyResume(checkpoint *SyncCheckpoint) error {
	if checkpoint == nil {
		return nil
	}

	if checkpoint.Cursor == "" {
		return fmt.Errorf("github sync cursor is missing: %w", ErrSyncResumeInvalid)
	}
	var cursor githubSyncCursor
	if err := json.Unmarshal([]byte(checkpoint.Cursor), &cursor); err != nil {
		return fmt.Errorf("github sync cursor is invalid: %w", ErrSyncResumeInvalid)
	}
	if cursor.Repo == "" || cursor.Stage == "" || cursor.Page <= 0 {
		return fmt.Errorf("github sync cursor has no resume anchor: %w", ErrSyncResumeInvalid)
	}
	for index, repo := range s.repos {
		if repo.FullName != cursor.Repo {
			continue
		}
		s.repoIndex = index
		s.stage = cursor.Stage
		s.page = cursor.Page
		s.resumeRepo = cursor.Repo
		s.resumeStage = cursor.Stage
		s.resumePage = cursor.Page
		s.resumeOffset = cursor.Offset
		s.resumeSourceID = firstNonEmpty(cursor.SourceID, checkpoint.SourceID)
		if s.resumeSourceID == "" {
			return fmt.Errorf("github sync checkpoint has no source anchor: %w", ErrSyncResumeInvalid)
		}
		return nil
	}
	return fmt.Errorf("github resume repo %q was not found in the current listing: %w", cursor.Repo, ErrSyncResumeInvalid)
}

// filterResumedDocuments drops documents through the committed checkpoint.
func (s *githubSyncSession) filterResumedDocuments(repo, stage string, page int, candidates []githubBufferedDocument) ([]githubBufferedDocument, error) {
	if s.resumeRepo == "" || repo != s.resumeRepo || stage != s.resumeStage || page != s.resumePage {
		return candidates, nil
	}
	if s.resumeSourceID != "" {
		for index, candidate := range candidates {
			if candidate.sourceID == s.resumeSourceID {
				s.clearResume()
				return candidates[index+1:], nil
			}
		}
		return nil, fmt.Errorf("github resume anchor %q was not found on page %d: %w", s.resumeSourceID, page, ErrSyncResumeInvalid)
	}
	return nil, fmt.Errorf("github sync cursor has no source anchor: %w", ErrSyncResumeInvalid)
}

func (s *githubSyncSession) clearResume() {
	s.resumeRepo = ""
	s.resumeStage = ""
	s.resumePage = 0
	s.resumeOffset = 0
	s.resumeSourceID = ""
}

// advanceStage moves a GitHub prune session to the next stage within the same repository.
func (s *githubPruneSession) advanceStage() {
	switch s.stage {
	case githubStageCodeFiles:
		s.stage = githubStagePRs
	case githubStagePRs:
		s.stage = githubStageIssues
	default:
		s.advanceRepo()
		return
	}
	s.page = 1
	s.codeFiles = nil
	s.codeFilesBranch = ""
	s.codeFilesLoaded = false
}

// advanceRepo moves a GitHub prune session to the next repository.
func (s *githubPruneSession) advanceRepo() {
	s.repoIndex++
	s.stage = s.connector.initialStage()
	s.page = 1
	s.codeFiles = nil
	s.codeFilesBranch = ""
	s.codeFilesLoaded = false
}

type githubRepo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

type githubTreeResponse struct {
	SHA       string            `json:"sha"`
	Tree      []githubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type githubFetchReference struct {
	FullName string `json:"full_name"`
	Branch   string `json:"branch"`
	Path     string `json:"path"`
	SHA      string `json:"sha"`
}

// shouldExcludeGitHubPath reports whether a repository path is inside a noisy build/dependency folder.
func shouldExcludeGitHubPath(filePath string) bool {
	parts := strings.Split(strings.Trim(filePath, "/"), "/")
	for _, part := range parts {
		for _, excluded := range githubExcludePatterns {
			if strings.EqualFold(part, excluded) {
				return true
			}
		}
	}
	return false
}

// githubFileExt returns the lowercased extension (with leading dot) for a file path.
func githubFileExt(filePath string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(filePath)))
	return ext
}

var githubSupportedExtensions = map[string]struct{}{
	".pdf": {}, ".doc": {}, ".docx": {}, ".ppt": {}, ".pptx": {}, ".xls": {}, ".xlsx": {},
	".md": {}, ".markdown": {}, ".mdx": {}, ".txt": {}, ".rst": {}, ".rtf": {},
	".csv": {}, ".json": {}, ".jsonl": {}, ".ldjson": {}, ".yml": {}, ".yaml": {}, ".xml": {}, ".ini": {}, ".toml": {},
	".htm": {}, ".html": {}, ".sql": {}, ".epub": {}, ".msg": {}, ".eml": {},
	".wps": {}, ".wpt": {}, ".et": {}, ".ett": {}, ".dps": {}, ".dpt": {}, ".hlp": {}, ".pages": {}, ".numbers": {}, ".key": {},
	".py": {}, ".js": {}, ".jsx": {}, ".ts": {}, ".tsx": {}, ".java": {}, ".c": {}, ".cpp": {}, ".h": {}, ".hpp": {},
	".php": {}, ".go": {}, ".sh": {}, ".cs": {}, ".kt": {}, ".rs": {}, ".rb": {}, ".swift": {}, ".scala": {},
	".jpg": {}, ".jpeg": {}, ".png": {}, ".tif": {}, ".gif": {}, ".bmp": {}, ".svg": {}, ".webp": {},
}

// isSupportedGitHubCodeFile reports whether a repository file should be ingested by RAGFlow.
func isSupportedGitHubCodeFile(filePath string) bool {
	ext := githubFileExt(filePath)
	if ext == "" {
		return false
	}
	_, ok := githubSupportedExtensions[ext]
	return ok
}

// githubAPIError is a GitHub API error with its HTTP status.
type githubAPIError struct {
	Status  int
	Message string
}

func (e *githubAPIError) Error() string {
	if e.Status == 0 {
		return e.Message
	}
	return fmt.Sprintf("GitHub API returned HTTP %d: %s", e.Status, e.Message)
}

type githubSyncCursor struct {
	Repo     string `json:"repo"`
	Stage    string `json:"stage"`
	Page     int    `json:"page"`
	Offset   int    `json:"offset"`
	SourceID string `json:"source_id"`
}

type githubBufferedDocument struct {
	document   SourceDocument
	checkpoint *SyncCheckpoint
	offset     int
	sourceID   string
}

func githubSyncCheckpoint(repo, stage string, page, offset int, doc SourceDocument) *SyncCheckpoint {
	cursor, err := json.Marshal(githubSyncCursor{
		Repo:     repo,
		Stage:    stage,
		Page:     page,
		Offset:   offset,
		SourceID: doc.SourceID,
	})
	if err != nil {
		return nil
	}
	updatedAt := doc.UpdatedAt
	return &SyncCheckpoint{
		Cursor:    string(cursor),
		SourceID:  doc.SourceID,
		UpdatedAt: &updatedAt,
	}
}

type githubPullRequest struct {
	HTMLURL   string        `json:"html_url"`
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	State     string        `json:"state"`
	MergedAt  *time.Time    `json:"merged_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	CreatedAt *time.Time    `json:"created_at"`
	ClosedAt  *time.Time    `json:"closed_at"`
	User      *githubUser   `json:"user"`
	Assignees []githubUser  `json:"assignees"`
	Labels    []githubLabel `json:"labels"`
}

// toSourceDocument converts a pull request into the syncer model.
func (p githubPullRequest) toSourceDocument(repo string) SourceDocument {
	body := []byte(p.Body)
	labels := githubLabelNames(p.Labels)
	user := p.User.metadata()
	assignees := githubUsersMetadata(p.Assignees)
	return SourceDocument{
		SourceID:           p.HTMLURL,
		SemanticIdentifier: fmt.Sprintf("%d:%s", p.Number, sanitizeGitHubName(p.Title, "md")),
		Extension:          ".md",
		Blob:               body,
		UpdatedAt:          p.UpdatedAt.UTC(),
		SizeBytes:          int64(len(body)),
		Metadata: map[string]any{
			"object_type": "PullRequest",
			"id":          strconv.Itoa(p.Number),
			"state":       p.State,
			"repo":        repo,
			"merged":      strconv.FormatBool(p.MergedAt != nil),
			"labels":      labels,
			"user":        user,
			"assignees":   assignees,
		},
		Fingerprint: stableFingerprint(map[string]any{
			"type":       "PullRequest",
			"url":        p.HTMLURL,
			"title":      p.Title,
			"body":       p.Body,
			"state":      p.State,
			"updated_at": p.UpdatedAt.UTC(),
			"merged_at":  p.MergedAt,
			"labels":     labels,
			"user":       user,
			"assignees":  assignees,
		}),
	}
}

type githubIssue struct {
	HTMLURL     string        `json:"html_url"`
	Number      int           `json:"number"`
	Title       string        `json:"title"`
	Body        string        `json:"body"`
	State       string        `json:"state"`
	UpdatedAt   time.Time     `json:"updated_at"`
	CreatedAt   *time.Time    `json:"created_at"`
	ClosedAt    *time.Time    `json:"closed_at"`
	User        *githubUser   `json:"user"`
	Assignees   []githubUser  `json:"assignees"`
	Labels      []githubLabel `json:"labels"`
	PullRequest *struct{}     `json:"pull_request"`
}

// toSourceDocument converts an issue into the syncer model.
func (i githubIssue) toSourceDocument(repo string) SourceDocument {
	body := []byte(i.Body)
	labels := githubLabelNames(i.Labels)
	user := i.User.metadata()
	assignees := githubUsersMetadata(i.Assignees)
	return SourceDocument{
		SourceID:           i.HTMLURL,
		SemanticIdentifier: fmt.Sprintf("%d:%s", i.Number, sanitizeGitHubName(i.Title, "md")),
		Extension:          ".md",
		Blob:               body,
		UpdatedAt:          i.UpdatedAt.UTC(),
		SizeBytes:          int64(len(body)),
		Metadata: map[string]any{
			"object_type": "Issue",
			"id":          strconv.Itoa(i.Number),
			"state":       i.State,
			"repo":        repo,
			"labels":      labels,
			"user":        user,
			"assignees":   assignees,
		},
		Fingerprint: stableFingerprint(map[string]any{
			"type":       "Issue",
			"url":        i.HTMLURL,
			"title":      i.Title,
			"body":       i.Body,
			"state":      i.State,
			"updated_at": i.UpdatedAt.UTC(),
			"closed_at":  i.ClosedAt,
			"labels":     labels,
			"user":       user,
			"assignees":  assignees,
		}),
	}
}

type githubUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// metadata returns Python-like user metadata.
func (u *githubUser) metadata() map[string]string {
	if u == nil {
		return nil
	}
	out := map[string]string{}
	if u.Login != "" {
		out["login"] = u.Login
	}
	if u.Name != "" {
		out["name"] = u.Name
	}
	if u.Email != "" {
		out["email"] = u.Email
	}
	return out
}

type githubLabel struct {
	Name string `json:"name"`
}

// githubLabelNames returns label names.
func githubLabelNames(labels []githubLabel) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		if label.Name != "" {
			out = append(out, label.Name)
		}
	}
	sort.Strings(out)
	return out
}

// githubUsersMetadata returns metadata for users.
func githubUsersMetadata(users []githubUser) []map[string]string {
	users = append([]githubUser(nil), users...)
	sort.Slice(users, func(i, j int) bool {
		return githubUserSortKey(users[i]) < githubUserSortKey(users[j])
	})
	out := make([]map[string]string, 0, len(users))
	for i := range users {
		out = append(out, (&users[i]).metadata())
	}
	return out
}

func githubUserSortKey(user githubUser) string {
	if user.Login != "" {
		return user.Login
	}
	if user.Email != "" {
		return user.Email
	}
	return user.Name
}

// hasNextPage reports whether a GitHub Link header has rel next.
func hasNextPage(headers http.Header) bool {
	return strings.Contains(headers.Get("Link"), `rel="next"`)
}

// beforeOrAtWindowStart reports whether an updated time is outside the lower bound.
func beforeOrAtWindowStart(updatedAt time.Time, windowStart *time.Time) bool {
	return windowStart != nil && !updatedAt.After(*windowStart)
}

// afterWindowEnd reports whether an updated time is outside the upper bound.
func afterWindowEnd(updatedAt time.Time, windowEnd time.Time) bool {
	return !windowEnd.IsZero() && updatedAt.After(windowEnd)
}

// splitGitHubRepos parses Python's comma-separated repository_name config.
func splitGitHubRepos(value string) []string {
	parts := strings.Split(value, ",")
	repos := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			repos = append(repos, part)
		}
	}
	return repos
}

// stringConfig reads a string config value.
func stringConfig(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// configBoolDefault reads a Python JSON bool/string flag with a default.
func configBoolDefault(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}

// sanitizeGitHubName mirrors Python's sanitized Markdown filename intent.
func sanitizeGitHubName(name, extension string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "github"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")
	name = replacer.Replace(name)
	if !strings.HasSuffix(strings.ToLower(name), "."+extension) {
		name += "." + extension
	}
	return name
}
