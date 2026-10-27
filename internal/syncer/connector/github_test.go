package connector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestGitHubConnectorOpenSyncUsesWindowAndFingerprint verifies incremental sync emits only updated docs with fingerprints.
func TestGitHubConnectorOpenSyncUsesWindowAndFingerprint(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner":      "openai",
		"repository_name":       "ragflow",
		"include_pull_requests": true,
		"include_issues":        true,
		"batch_size":            10,
		"credentials":           map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubFixtureDoJSON(t)

	start := mustTime(t, "2026-01-02T12:00:00Z")
	end := mustTime(t, "2026-01-04T00:00:00Z")
	session, err := connector.OpenSync(t.Context(), SyncRequest{WindowStart: &start, WindowEnd: end})
	if err != nil {
		t.Fatalf("OpenSync failed: %v", err)
	}
	batch, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch failed: %v", err)
	}
	if len(batch.Documents) != 1 {
		t.Fatalf("documents len = %d, want 1", len(batch.Documents))
	}
	doc := batch.Documents[0]
	if doc.SourceID != "https://github.com/openai/ragflow/pull/7" {
		t.Fatalf("source id = %q", doc.SourceID)
	}
	if doc.Fingerprint == "" {
		t.Fatalf("fingerprint is empty")
	}
	if _, err = session.NextBatch(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("NextBatch EOF = %v", err)
	}
}

// TestGitHubFingerprintStable verifies GitHub fingerprints are stable and content-sensitive.
func TestGitHubFingerprintStable(t *testing.T) {
	updatedAt := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	pr := githubPullRequest{
		HTMLURL:   "https://github.com/openai/ragflow/pull/7",
		Number:    7,
		Title:     "Add syncer",
		Body:      "PR body",
		State:     "open",
		UpdatedAt: updatedAt,
		User:      &githubUser{Login: "alice"},
		Assignees: []githubUser{
			{Login: "zoe"},
			{Login: "bob"},
		},
		Labels: []githubLabel{
			{Name: "sync"},
			{Name: "bug"},
		},
	}
	fp1 := pr.toSourceDocument("openai/ragflow").Fingerprint
	fp2 := pr.toSourceDocument("openai/ragflow").Fingerprint
	if fp1 == "" || fp1 != fp2 {
		t.Fatalf("fingerprint unstable: %q %q", fp1, fp2)
	}

	reordered := pr
	reordered.Labels = []githubLabel{{Name: "bug"}, {Name: "sync"}}
	reordered.Assignees = []githubUser{{Login: "bob"}, {Login: "zoe"}}
	if got := reordered.toSourceDocument("openai/ragflow").Fingerprint; got != fp1 {
		t.Fatalf("fingerprint changed after order-only change: %q != %q", got, fp1)
	}

	changed := pr
	changed.Title = "Add syncer v2"
	if got := changed.toSourceDocument("openai/ragflow").Fingerprint; got == fp1 {
		t.Fatalf("fingerprint did not change after title update")
	}
}

// TestGitHubConnectorOpenPrune verifies PRUNE returns Python-compatible html_url IDs.
func TestGitHubConnectorOpenPrune(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner":      "openai",
		"repository_name":       "ragflow",
		"include_pull_requests": true,
		"include_issues":        true,
		"batch_size":            10,
		"credentials":           map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubFixtureDoJSON(t)

	session, err := connector.OpenPrune(t.Context(), PruneRequest{})
	if err != nil {
		t.Fatalf("OpenPrune failed: %v", err)
	}
	batch, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch failed: %v", err)
	}
	got := []string{}
	for _, doc := range batch.Documents {
		got = append(got, doc.SourceID)
	}
	want := []string{
		"https://github.com/openai/ragflow/pull/7",
		"https://github.com/openai/ragflow/issues/3",
	}
	if len(got) != len(want) {
		t.Fatalf("ids len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestGitHubConnectorOpenSyncResumesAfterCheckpoint verifies retry skips committed GitHub documents.
func TestGitHubConnectorOpenSyncResumesAfterCheckpoint(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner":      "openai",
		"repository_name":       "ragflow",
		"include_pull_requests": true,
		"include_issues":        true,
		"batch_size":            1,
		"credentials":           map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubFixtureDoJSON(t)

	end := mustTime(t, "2026-01-04T00:00:00Z")
	session, err := connector.OpenSync(t.Context(), SyncRequest{FromBeginning: true, WindowEnd: end})
	if err != nil {
		t.Fatalf("OpenSync failed: %v", err)
	}
	first, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch first failed: %v", err)
	}
	if len(first.Documents) != 1 || first.Documents[0].SourceID != "https://github.com/openai/ragflow/pull/7" {
		t.Fatalf("first documents = %+v, want PR 7", first.Documents)
	}
	if first.Checkpoint == nil || first.Checkpoint.SourceID != "https://github.com/openai/ragflow/pull/7" {
		t.Fatalf("first checkpoint = %+v, want PR 7", first.Checkpoint)
	}

	resumed, err := connector.OpenSync(t.Context(), SyncRequest{FromBeginning: true, WindowEnd: end, Resume: first.Checkpoint})
	if err != nil {
		t.Fatalf("resume OpenSync failed: %v", err)
	}
	second, err := resumed.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("resume NextBatch failed: %v", err)
	}
	if len(second.Documents) != 1 || second.Documents[0].SourceID != "https://github.com/openai/ragflow/issues/3" {
		t.Fatalf("resume documents = %+v, want issue 3", second.Documents)
	}
	if second.Checkpoint == nil || second.Checkpoint.SourceID != "https://github.com/openai/ragflow/issues/3" {
		t.Fatalf("resume checkpoint = %+v, want issue 3", second.Checkpoint)
	}
	if _, err = resumed.NextBatch(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("resume EOF = %v", err)
	}
}

// TestGitHubConnectorOpenSyncResumeRejectsMissingSourceAnchor verifies a checkpoint without a source anchor is invalid.
func TestGitHubConnectorOpenSyncResumeRejectsMissingSourceAnchor(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner":      "openai",
		"repository_name":       "ragflow",
		"include_pull_requests": true,
		"include_issues":        true,
		"batch_size":            1,
		"credentials":           map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubFixtureDoJSON(t)

	end := mustTime(t, "2026-01-04T00:00:00Z")
	session, err := connector.OpenSync(t.Context(), SyncRequest{FromBeginning: true, WindowEnd: end})
	if err != nil {
		t.Fatalf("OpenSync failed: %v", err)
	}
	first, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch first failed: %v", err)
	}
	if len(first.Documents) != 1 || first.Documents[0].SourceID != "https://github.com/openai/ragflow/pull/7" {
		t.Fatalf("first documents = %+v, want PR 7", first.Documents)
	}
	if first.Checkpoint == nil {
		t.Fatalf("first checkpoint is nil")
	}

	resumeCheckpoint := cloneGitHubCheckpointWithMissingSourceID(t, first.Checkpoint)
	resumed, err := connector.OpenSync(t.Context(), SyncRequest{FromBeginning: true, WindowEnd: end, Resume: resumeCheckpoint})
	if resumed != nil || err == nil || !errors.Is(err, ErrSyncResumeInvalid) {
		t.Fatalf("resume OpenSync = session %v, err %v, want ErrSyncResumeInvalid", resumed, err)
	}
}

func cloneGitHubCheckpointWithMissingSourceID(t *testing.T, checkpoint *SyncCheckpoint) *SyncCheckpoint {
	t.Helper()
	var cursor githubSyncCursor
	if err := json.Unmarshal([]byte(checkpoint.Cursor), &cursor); err != nil {
		t.Fatalf("decode checkpoint cursor: %v", err)
	}
	cursor.SourceID = ""
	data, err := json.Marshal(cursor)
	if err != nil {
		t.Fatalf("encode checkpoint cursor: %v", err)
	}
	clone := *checkpoint
	clone.Cursor = string(data)
	clone.SourceID = ""
	return &clone
}

// githubFixtureDoJSON returns a fixture GitHub JSON transport.
func githubFixtureDoJSON(t *testing.T) func(ctx context.Context, apiURL string, out any) (http.Header, error) {
	t.Helper()
	return func(ctx context.Context, apiURL string, out any) (http.Header, error) {
		parsed, err := url.Parse(apiURL)
		if err != nil {
			t.Fatalf("parse api url: %v", err)
		}
		fixtures := map[string]string{
			"/repos/openai/ragflow": `{"full_name":"openai/ragflow"}`,
			"/repos/openai/ragflow/pulls": `[{
				"html_url":"https://github.com/openai/ragflow/pull/7",
				"number":7,
				"title":"Add syncer",
				"body":"PR body",
				"state":"open",
				"updated_at":"2026-01-03T00:00:00Z",
				"labels":[{"name":"sync"}],
				"user":{"login":"alice"}
			}]`,
			"/repos/openai/ragflow/issues": `[{
				"html_url":"https://github.com/openai/ragflow/issues/3",
				"number":3,
				"title":"Prune bug",
				"body":"Issue body",
				"state":"open",
				"updated_at":"2026-01-02T00:00:00Z",
				"labels":[{"name":"bug"}],
				"user":{"login":"bob"}
			},{
				"html_url":"https://github.com/openai/ragflow/pull/7",
				"number":7,
				"title":"PR shadow",
				"body":"skip me",
				"state":"open",
				"updated_at":"2026-01-03T00:00:00Z",
				"pull_request":{}
			}]`,
		}
		body, ok := fixtures[parsed.Path]
		if !ok {
			t.Fatalf("unexpected api path %s", parsed.Path)
		}
		if err = json.Unmarshal([]byte(body), out); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		return http.Header{}, nil
	}
}

// githubValidationDoJSON returns a GitHub fixture transport for connection testing.
func githubValidationDoJSON(t *testing.T, ok map[string]string, fail map[string]*githubAPIError) func(ctx context.Context, apiURL string, out any) (http.Header, error) {
	t.Helper()
	return func(ctx context.Context, apiURL string, out any) (http.Header, error) {
		parsed, err := url.Parse(apiURL)
		if err != nil {
			t.Fatalf("parse api url: %v", err)
		}
		if strings.HasPrefix(parsed.Path, "/repos/") || strings.Contains(parsed.Path, "/contents") {
			t.Fatalf("ValidateConnectorSetting should not call repository or contents endpoint: %s", parsed.Path)
		}
		if apiErr, exists := fail[parsed.Path]; exists {
			return nil, apiErr
		}
		body, exists := ok[parsed.Path]
		if !exists {
			t.Fatalf("unexpected api path %s", parsed.Path)
		}
		if err = json.Unmarshal([]byte(body), out); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		return http.Header{}, nil
	}
}

// TestGitHubValidateConnectorSettingSingleRepoSkipsRepoAccess verifies test
// connection does not probe repository or contents endpoints.
func TestGitHubValidateConnectorSettingSingleRepoSkipsRepoAccess(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner": "openai",
		"repository_name":  "ragflow",
		"credentials":      map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubValidationDoJSON(t,
		map[string]string{
			"/orgs/openai":       `{"login":"openai"}`,
			"/orgs/openai/repos": `[{"full_name":"openai/ragflow"}]`,
		},
		nil,
	)
	if err := connector.ValidateConnectorSetting(t.Context(), nil); err != nil {
		t.Fatalf("ValidateConnectorSetting: %v", err)
	}
}

// TestGitHubValidateConnectorSettingMultipleReposSkipsRepoAccess verifies comma-separated repository names
// do not trigger repository-level access probes during test connection.
func TestGitHubValidateConnectorSettingMultipleReposSkipsRepoAccess(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner": "openai",
		"repository_name":  "missing, ragflow",
		"credentials":      map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubValidationDoJSON(t,
		map[string]string{
			"/orgs/openai":       `{"login":"openai"}`,
			"/orgs/openai/repos": `[{"full_name":"openai/ragflow"}]`,
		},
		nil,
	)
	if err := connector.ValidateConnectorSetting(t.Context(), nil); err != nil {
		t.Fatalf("ValidateConnectorSetting: %v", err)
	}
}

// TestGitHubValidateConnectorSettingUnauthorized verifies an invalid token is detected.
func TestGitHubValidateConnectorSettingUnauthorized(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner": "openai",
		"repository_name":  "ragflow",
		"credentials":      map[string]any{"github_access_token": "bad-token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubValidationDoJSON(t,
		nil,
		map[string]*githubAPIError{
			"/orgs/openai": {Status: http.StatusUnauthorized, Message: `{"message":"Bad credentials"}`},
		},
	)
	err = connector.ValidateConnectorSetting(t.Context(), nil)
	var credErr *ConnectorMissingCredentialError
	if !errors.As(err, &credErr) {
		t.Fatalf("err = %v, want ConnectorMissingCredentialError", err)
	}
}

// TestGitHubValidateConnectorSettingOwnerOrg verifies an owner with repositories passes.
func TestGitHubValidateConnectorSettingOwnerOrg(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner": "openai",
		"credentials":      map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubValidationDoJSON(t,
		map[string]string{
			"/orgs/openai":       `{"login":"openai"}`,
			"/orgs/openai/repos": `[{"full_name":"openai/ragflow"}]`,
		},
		nil,
	)
	if err := connector.ValidateConnectorSetting(t.Context(), nil); err != nil {
		t.Fatalf("ValidateConnectorSetting: %v", err)
	}
}

// TestGitHubValidateConnectorSettingOwnerUserFallback verifies a user owner falls back from org lookup.
func TestGitHubValidateConnectorSettingOwnerUserFallback(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner": "openai",
		"credentials":      map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubValidationDoJSON(t,
		map[string]string{
			"/users/openai":       `{"login":"openai"}`,
			"/users/openai/repos": `[{"full_name":"openai/ragflow"}]`,
		},
		map[string]*githubAPIError{
			"/orgs/openai": {Status: http.StatusNotFound, Message: `{"message":"Not Found"}`},
		},
	)
	if err := connector.ValidateConnectorSetting(t.Context(), nil); err != nil {
		t.Fatalf("ValidateConnectorSetting: %v", err)
	}
}

// TestGitHubValidateConnectorSettingOwnerNotFound verifies an unknown owner fails.
func TestGitHubValidateConnectorSettingOwnerNotFound(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner": "ghost",
		"credentials":      map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubValidationDoJSON(t,
		nil,
		map[string]*githubAPIError{
			"/orgs/ghost":  {Status: http.StatusNotFound, Message: `{"message":"Not Found"}`},
			"/users/ghost": {Status: http.StatusNotFound, Message: `{"message":"Not Found"}`},
		},
	)
	err = connector.ValidateConnectorSetting(t.Context(), nil)
	var valErr *ConnectorValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("err = %v, want ConnectorValidationError", err)
	}
	if got, want := valErr.Message, "GitHub user or organization not found: ghost"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

// TestGitHubValidateConnectorSettingMissingSSO verifies the SSO guide error is reported.
func TestGitHubValidateConnectorSettingMissingSSO(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner": "ssoorg",
		"credentials":      map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = githubValidationDoJSON(t,
		nil,
		map[string]*githubAPIError{
			"/orgs/ssoorg": {Status: http.StatusForbidden, Message: `{"message":"Resource protected by organization SAML enforcement. You must grant your Personal Access token access to this organization."}`},
		},
	)
	err = connector.ValidateConnectorSetting(t.Context(), nil)
	var valErr *ConnectorValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("err = %v, want ConnectorValidationError", err)
	}
	if !strings.Contains(valErr.Message, "missing authorization") {
		t.Fatalf("message = %q, want SSO authorization hint", valErr.Message)
	}
}

// TestGitHubValidateConnectorSettingMissingInputs verifies missing config errors.
func TestGitHubValidateConnectorSettingMissingInputs(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		connector, err := NewGitHubConnector(map[string]any{
			"repository_owner": "openai",
			"repository_name":  "ragflow",
			"credentials":      map[string]any{},
		})
		if err != nil {
			t.Fatalf("NewGitHubConnector failed: %v", err)
		}
		err = connector.ValidateConnectorSetting(t.Context(), nil)
		var credErr *ConnectorMissingCredentialError
		if !errors.As(err, &credErr) {
			t.Fatalf("err = %v, want ConnectorMissingCredentialError", err)
		}
	})
	t.Run("missing owner", func(t *testing.T) {
		connector, err := NewGitHubConnector(map[string]any{
			"repository_name": "ragflow",
			"credentials":     map[string]any{"github_access_token": "token"},
		})
		if err != nil {
			t.Fatalf("NewGitHubConnector failed: %v", err)
		}
		err = connector.ValidateConnectorSetting(t.Context(), nil)
		var valErr *ConnectorValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("err = %v, want ConnectorValidationError", err)
		}
	})
}

// TestGitHubConnectorCodeFilesDirectorySyncAndMetaFields verifies syncing a specific folder (e.g. docs/),
// filtering supported files, relative SemanticIdentifier naming, web_url metadata, and lazy blob fetching.
func TestGitHubConnectorCodeFilesDirectorySyncAndMetaFields(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner":      "hung2604",
		"repository_name":       "BA-Demego",
		"include_pull_requests": false,
		"include_issues":        false,
		"include_code_files":    true,
		"branch":                "",
		"sync_scope":            "directory",
		"folder_path":           "/docs/",
		"batch_size":            10,
		"credentials":           map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = func(ctx context.Context, apiURL string, out any) (http.Header, error) {
		parsed, err := url.Parse(apiURL)
		if err != nil {
			t.Fatalf("parse api url: %v", err)
		}
		switch parsed.Path {
		case "/repos/hung2604/BA-Demego":
			return http.Header{}, json.Unmarshal([]byte(`{"full_name":"hung2604/BA-Demego","default_branch":"main"}`), out)
		case "/repos/hung2604/BA-Demego/git/trees/main":
			treeJSON := `{
				"sha": "root-sha",
				"truncated": false,
				"tree": [
					{"path": "README.md", "mode": "100644", "type": "blob", "sha": "sha-readme", "size": 120},
					{"path": "docs", "mode": "040000", "type": "tree", "sha": "sha-docs-tree", "size": 0},
					{"path": "docs/23-dien-nuoc.md", "mode": "100644", "type": "blob", "sha": "sha-23", "size": 512},
					{"path": "docs/sub/api-spec.yaml", "mode": "100644", "type": "blob", "sha": "sha-yaml", "size": 256},
					{"path": "docs/node_modules/pkg.md", "mode": "100644", "type": "blob", "sha": "sha-nm", "size": 100},
					{"path": "docs/demo.mp4", "mode": "100644", "type": "blob", "sha": "sha-video", "size": 1024},
					{"path": "docs/submodule", "mode": "160000", "type": "blob", "sha": "sha-submod", "size": 0},
					{"path": "docs/huge.pdf", "mode": "100644", "type": "blob", "sha": "sha-huge", "size": 25000000}
				]
			}`
			return http.Header{}, json.Unmarshal([]byte(treeJSON), out)
		default:
			t.Fatalf("unexpected api path %s", parsed.Path)
			return nil, nil
		}
	}

	blobCalls := 0
	connector.doRaw = func(ctx context.Context, apiURL string) ([]byte, error) {
		blobCalls++
		parsed, err := url.Parse(apiURL)
		if err != nil {
			t.Fatalf("parse raw url: %v", err)
		}
		if parsed.Path == "/repos/hung2604/BA-Demego/git/blobs/sha-23" {
			return []byte("# Quy trinh Dien Nuoc"), nil
		}
		if parsed.Path == "/repos/hung2604/BA-Demego/git/blobs/sha-yaml" {
			return []byte("openapi: 3.0.0"), nil
		}
		t.Fatalf("unexpected blob path %s", parsed.Path)
		return nil, nil
	}

	end := mustTime(t, "2026-01-04T00:00:00Z")
	session, err := connector.OpenSync(t.Context(), SyncRequest{WindowEnd: end})
	if err != nil {
		t.Fatalf("OpenSync failed: %v", err)
	}
	batch, err := session.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("NextBatch failed: %v", err)
	}
	// Verify NextBatch is lazy: no blob download happens during NextBatch
	if blobCalls != 0 {
		t.Fatalf("blobCalls during NextBatch = %d, want 0", blobCalls)
	}
	if len(batch.Documents) != 2 {
		t.Fatalf("documents len = %d, want 2: %+v", len(batch.Documents), batch.Documents)
	}

	doc0 := batch.Documents[0]
	if doc0.SemanticIdentifier != "23-dien-nuoc.md" {
		t.Fatalf("doc0 SemanticIdentifier = %q, want 23-dien-nuoc.md", doc0.SemanticIdentifier)
	}
	if doc0.SourceID != "https://github.com/hung2604/BA-Demego/blob/main/docs/23-dien-nuoc.md" {
		t.Fatalf("doc0 SourceID = %q", doc0.SourceID)
	}
	if doc0.Metadata["web_url"] != "https://github.com/hung2604/BA-Demego/blob/main/docs/23-dien-nuoc.md" {
		t.Fatalf("doc0 web_url = %v", doc0.Metadata["web_url"])
	}
	if doc0.Metadata["path"] != "docs/23-dien-nuoc.md" || doc0.Metadata["branch"] != "main" || doc0.Metadata["object_type"] != "CodeFile" {
		t.Fatalf("doc0 metadata = %+v", doc0.Metadata)
	}

	doc1 := batch.Documents[1]
	if doc1.SemanticIdentifier != "sub_api-spec.yaml" {
		t.Fatalf("doc1 SemanticIdentifier = %q, want sub_api-spec.yaml", doc1.SemanticIdentifier)
	}

	// Verify Fetch downloads raw blob by SHA
	fetcher, ok := session.(Fetcher)
	if !ok {
		t.Fatalf("session does not implement Fetcher")
	}
	rawBytes, err := fetcher.Fetch(context.Background(), *doc0.FetchRef)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if string(rawBytes) != "# Quy trinh Dien Nuoc" {
		t.Fatalf("fetched content = %q", string(rawBytes))
	}
	if blobCalls != 1 {
		t.Fatalf("blobCalls = %d, want 1", blobCalls)
	}

	if _, err = session.NextBatch(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("NextBatch EOF = %v", err)
	}

	// Verify OpenPrune returns matching SourceIDs for the 2 valid files in docs/
	pruneSession, err := connector.OpenPrune(t.Context(), PruneRequest{})
	if err != nil {
		t.Fatalf("OpenPrune failed: %v", err)
	}
	pruneBatch, err := pruneSession.NextBatch(context.Background())
	if err != nil {
		t.Fatalf("Prune NextBatch failed: %v", err)
	}
	if len(pruneBatch.Documents) != 2 {
		t.Fatalf("prune docs len = %d, want 2", len(pruneBatch.Documents))
	}
	if pruneBatch.Documents[0].SourceID != doc0.SourceID || pruneBatch.Documents[1].SourceID != doc1.SourceID {
		t.Fatalf("prune source IDs = %+v", pruneBatch.Documents)
	}
}

// TestGitHubConnectorCodeFilesTruncatedTreeAbortsSyncAndPrune verifies truncated=true aborts both sync and prune.
func TestGitHubConnectorCodeFilesTruncatedTreeAbortsSyncAndPrune(t *testing.T) {
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner":      "hung2604",
		"repository_name":       "BA-Demego",
		"include_pull_requests": false,
		"include_issues":        false,
		"include_code_files":    true,
		"sync_scope":            "repository",
		"credentials":           map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = func(ctx context.Context, apiURL string, out any) (http.Header, error) {
		parsed, _ := url.Parse(apiURL)
		switch parsed.Path {
		case "/repos/hung2604/BA-Demego":
			return http.Header{}, json.Unmarshal([]byte(`{"full_name":"hung2604/BA-Demego","default_branch":"main"}`), out)
		case "/repos/hung2604/BA-Demego/git/trees/main":
			return http.Header{}, json.Unmarshal([]byte(`{"sha":"root","truncated":true,"tree":[]}`), out)
		default:
			t.Fatalf("unexpected path %s", parsed.Path)
			return nil, nil
		}
	}

	syncSession, err := connector.OpenSync(t.Context(), SyncRequest{WindowEnd: time.Now().UTC()})
	if err != nil {
		t.Fatalf("OpenSync failed: %v", err)
	}
	if _, err := syncSession.NextBatch(context.Background()); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("expected truncated error from Sync NextBatch, got %v", err)
	}

	pruneSession, err := connector.OpenPrune(t.Context(), PruneRequest{})
	if err != nil {
		t.Fatalf("OpenPrune failed: %v", err)
	}
	if _, err := pruneSession.NextBatch(context.Background()); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("expected truncated error from Prune NextBatch, got %v", err)
	}
}

// TestGitHubValidateConnectorSettingCodeFiles verifies validation of folder_path, sync_scope, and branch existence.
func TestGitHubValidateConnectorSettingCodeFiles(t *testing.T) {
	t.Run("all sources disabled fails", func(t *testing.T) {
		connector, err := NewGitHubConnector(map[string]any{
			"repository_owner":      "hung2604",
			"repository_name":       "BA-Demego",
			"include_pull_requests": false,
			"include_issues":        false,
			"include_code_files":    false,
			"credentials":           map[string]any{"github_access_token": "token"},
		})
		if err != nil {
			t.Fatalf("NewGitHubConnector failed: %v", err)
		}
		err = connector.ValidateConnectorSetting(t.Context(), nil)
		var valErr *ConnectorValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("err = %v, want ConnectorValidationError", err)
		}
	})

	t.Run("directory scope with empty folder_path fails", func(t *testing.T) {
		connector, err := NewGitHubConnector(map[string]any{
			"repository_owner":   "hung2604",
			"repository_name":    "BA-Demego",
			"include_code_files": true,
			"sync_scope":         "directory",
			"folder_path":        "   ",
			"credentials":        map[string]any{"github_access_token": "token"},
		})
		if err != nil {
			t.Fatalf("NewGitHubConnector failed: %v", err)
		}
		err = connector.ValidateConnectorSetting(t.Context(), nil)
		var valErr *ConnectorValidationError
		if !errors.As(err, &valErr) || !strings.Contains(valErr.Message, "folder_path") {
			t.Fatalf("err = %v, want folder_path ConnectorValidationError", err)
		}
	})

	t.Run("path traversal in folder_path fails", func(t *testing.T) {
		connector, err := NewGitHubConnector(map[string]any{
			"repository_owner":   "hung2604",
			"repository_name":    "BA-Demego",
			"include_code_files": true,
			"sync_scope":         "directory",
			"folder_path":        "docs/../secret",
			"credentials":        map[string]any{"github_access_token": "token"},
		})
		if err != nil {
			t.Fatalf("NewGitHubConnector failed: %v", err)
		}
		err = connector.ValidateConnectorSetting(t.Context(), nil)
		var valErr *ConnectorValidationError
		if !errors.As(err, &valErr) || !strings.Contains(valErr.Message, "..") {
			t.Fatalf("err = %v, want path traversal ConnectorValidationError", err)
		}
	})

	t.Run("valid directory exists in repo tree", func(t *testing.T) {
		connector, err := NewGitHubConnector(map[string]any{
			"repository_owner":   "hung2604",
			"repository_name":    "BA-Demego",
			"include_code_files": true,
			"branch":             "main",
			"sync_scope":         "directory",
			"folder_path":        "docs",
			"credentials":        map[string]any{"github_access_token": "token"},
		})
		if err != nil {
			t.Fatalf("NewGitHubConnector failed: %v", err)
		}
		connector.baseURL = "https://api.github.test"
		connector.doJSON = func(ctx context.Context, apiURL string, out any) (http.Header, error) {
			parsed, _ := url.Parse(apiURL)
			switch parsed.Path {
			case "/orgs/hung2604":
				return http.Header{}, json.Unmarshal([]byte(`{"login":"hung2604"}`), out)
			case "/orgs/hung2604/repos":
				return http.Header{}, json.Unmarshal([]byte(`[{"full_name":"hung2604/BA-Demego"}]`), out)
			case "/repos/hung2604/BA-Demego":
				return http.Header{}, json.Unmarshal([]byte(`{"full_name":"hung2604/BA-Demego","default_branch":"main"}`), out)
			case "/repos/hung2604/BA-Demego/git/trees/main":
				return http.Header{}, json.Unmarshal([]byte(`{"sha":"root","truncated":false,"tree":[{"path":"docs/23-dien-nuoc.md","mode":"100644","type":"blob","sha":"s1","size":10}]}`), out)
			default:
				t.Fatalf("unexpected path %s", parsed.Path)
				return nil, nil
			}
		}
		if err := connector.ValidateConnectorSetting(t.Context(), nil); err != nil {
			t.Fatalf("ValidateConnectorSetting failed: %v", err)
		}
	})
}

// TestGitHubConnectorCodeFilesIncrementalSHAFingerprintSkip verifies that when blob.SHA is unchanged across
// sync cycles, stored fingerprint matches in O(1) with 0 blob API calls, and only changed SHA files are fetched.
func TestGitHubConnectorCodeFilesIncrementalSHAFingerprintSkip(t *testing.T) {
	currentSHA23 := "sha-23-v1"
	connector, err := NewGitHubConnector(map[string]any{
		"repository_owner":      "hung2604",
		"repository_name":       "BA-Demego",
		"include_pull_requests": false,
		"include_issues":        false,
		"include_code_files":    true,
		"branch":                "main",
		"sync_scope":            "directory",
		"folder_path":           "docs",
		"credentials":           map[string]any{"github_access_token": "token"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector failed: %v", err)
	}
	connector.baseURL = "https://api.github.test"
	connector.doJSON = func(ctx context.Context, apiURL string, out any) (http.Header, error) {
		parsed, _ := url.Parse(apiURL)
		switch parsed.Path {
		case "/repos/hung2604/BA-Demego":
			return http.Header{}, json.Unmarshal([]byte(`{"full_name":"hung2604/BA-Demego","default_branch":"main"}`), out)
		case "/repos/hung2604/BA-Demego/git/trees/main":
			treeJSON := `{"sha":"root","truncated":false,"tree":[
				{"path":"docs/08-so-quy.md","mode":"100644","type":"blob","sha":"sha-08-v1","size":200},
				{"path":"docs/23-dien-nuoc.md","mode":"100644","type":"blob","sha":"` + currentSHA23 + `","size":300}
			]}`
			return http.Header{}, json.Unmarshal([]byte(treeJSON), out)
		default:
			t.Fatalf("unexpected path %s", parsed.Path)
			return nil, nil
		}
	}

	blobCalls := 0
	connector.doRaw = func(ctx context.Context, apiURL string) ([]byte, error) {
		blobCalls++
		return []byte("markdown content"), nil
	}

	storedFingerprints := map[string]string{}
	runCycle := func() int {
		before := blobCalls
		s, err := connector.OpenSync(t.Context(), SyncRequest{WindowEnd: time.Now().UTC()})
		if err != nil {
			t.Fatalf("OpenSync: %v", err)
		}
		batch, err := s.NextBatch(context.Background())
		if err != nil {
			t.Fatalf("NextBatch: %v", err)
		}
		fetcher := s.(Fetcher)
		for _, doc := range batch.Documents {
			if storedFingerprints[doc.SourceID] == doc.Fingerprint {
				continue
			}
			if _, err := fetcher.Fetch(context.Background(), *doc.FetchRef); err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			storedFingerprints[doc.SourceID] = doc.Fingerprint
		}
		return blobCalls - before
	}

	// Cycle 1: Initial sync downloads both files (2 blob calls)
	if calls := runCycle(); calls != 2 {
		t.Fatalf("Cycle 1 blob calls = %d, want 2", calls)
	}
	// Cycle 2: Unchanged SHA -> 0 blob calls (PERF-01)
	if calls := runCycle(); calls != 0 {
		t.Fatalf("Cycle 2 blob calls = %d, want 0", calls)
	}
	// Cycle 3: Update 1 file SHA -> 1 blob call
	currentSHA23 = "sha-23-v2"
	if calls := runCycle(); calls != 1 {
		t.Fatalf("Cycle 3 blob calls = %d, want 1", calls)
	}
}
