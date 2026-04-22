package jirax

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNormalizeIssueExtractsCoreFieldsCommentsAndChangelog(t *testing.T) {
	issue := sampleIssue()

	view, rawJSON, comments, changelog, err := normalizeIssue(issue)
	if err != nil {
		t.Fatalf("normalizeIssue() error = %v", err)
	}
	if view.Key != "DEMO-1" || view.Status != "In Progress" || view.Project != "DEMO" {
		t.Fatalf("unexpected normalized view: %+v", view)
	}
	if view.CustomFields["customfield_10010"] != "Sprint 4" {
		t.Fatalf("expected custom field to be preserved, got %+v", view.CustomFields)
	}
	if rawJSON == "" {
		t.Fatal("expected raw JSON")
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if len(changelog) != 1 {
		t.Fatalf("expected 1 changelog entry, got %d", len(changelog))
	}
}

func TestStoreRoundTripAndSearch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "jirax.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()
	if err := store.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if err := store.UpsertFieldCatalog(ctx, []FieldDefinition{
		{ID: "summary", Name: "Summary"},
		{ID: "customfield_10010", Name: "Sprint", Custom: true},
	}); err != nil {
		t.Fatalf("UpsertFieldCatalog() error = %v", err)
	}
	if err := store.UpsertIssueBundle(ctx, sampleIssue(), ContextConfig{Name: "default", Project: "DEMO"}); err != nil {
		t.Fatalf("UpsertIssueBundle() error = %v", err)
	}

	issue, err := store.GetIssue(ctx, "DEMO-1")
	if err != nil {
		t.Fatalf("GetIssue() error = %v", err)
	}
	if issue.Summary != "Sync local-first Jira issues" {
		t.Fatalf("unexpected issue summary: %+v", issue)
	}
	if len(issue.Comments) != 1 {
		t.Fatalf("expected comment round-trip, got %+v", issue.Comments)
	}

	results, err := store.SearchIssues(ctx, "local-first", 10)
	if err != nil {
		t.Fatalf("SearchIssues() error = %v", err)
	}
	if len(results) != 1 || results[0].Key != "DEMO-1" {
		t.Fatalf("unexpected search results: %+v", results)
	}

	jqlResults, err := store.SearchIssuesByJQL(ctx, `project = DEMO AND status = "In Progress" AND sprint = "Sprint 4"`, 10)
	if err != nil {
		t.Fatalf("SearchIssuesByJQL() error = %v", err)
	}
	if len(jqlResults) != 1 || jqlResults[0].Key != "DEMO-1" {
		t.Fatalf("unexpected JQL results: %+v", jqlResults)
	}
}

func TestStoreRecordSync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "jirax.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()
	if err := store.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := store.RecordSync(ctx, "default", now); err != nil {
		t.Fatalf("RecordSync() error = %v", err)
	}
	got, err := store.LastSyncTime(ctx, "default")
	if err != nil {
		t.Fatalf("LastSyncTime() error = %v", err)
	}
	if !got.Equal(now) {
		t.Fatalf("LastSyncTime() = %s, want %s", got, now)
	}
}

func TestStoreKnowledgeQueries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "jirax.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()
	if err := store.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if err := store.UpsertFieldCatalog(ctx, []FieldDefinition{
		{ID: "summary", Name: "Summary"},
		{ID: "status", Name: "Status"},
		{ID: "customfield_10010", Name: "Sprint", Custom: true},
	}); err != nil {
		t.Fatalf("UpsertFieldCatalog() error = %v", err)
	}
	if err := store.UpsertIssueBundle(ctx, sampleIssue(), ContextConfig{Name: "default", Project: "DEMO"}); err != nil {
		t.Fatalf("UpsertIssueBundle() error = %v", err)
	}

	fields, err := store.ListFields(ctx)
	if err != nil {
		t.Fatalf("ListFields() error = %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fields))
	}

	statuses, err := store.ListDistinctStatuses(ctx)
	if err != nil {
		t.Fatalf("ListDistinctStatuses() error = %v", err)
	}
	if len(statuses) != 1 || statuses[0] != "In Progress" {
		t.Fatalf("unexpected statuses: %+v", statuses)
	}

	types, err := store.ListDistinctIssueTypes(ctx)
	if err != nil {
		t.Fatalf("ListDistinctIssueTypes() error = %v", err)
	}
	if len(types) != 1 || types[0] != "Task" {
		t.Fatalf("unexpected issue types: %+v", types)
	}

	projects, err := store.ListDistinctProjects(ctx)
	if err != nil {
		t.Fatalf("ListDistinctProjects() error = %v", err)
	}
	if len(projects) != 1 || projects[0] != "DEMO" {
		t.Fatalf("unexpected projects: %+v", projects)
	}

	count, err := store.CountIssues(ctx)
	if err != nil {
		t.Fatalf("CountIssues() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 issue, got %d", count)
	}
}

func TestStoreSearchIssuesByJQLSupportsComplexFiltering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "jirax.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()
	if err := store.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if err := store.UpsertFieldCatalog(ctx, []FieldDefinition{
		{ID: "customfield_10010", Name: "Sprint", Custom: true},
	}); err != nil {
		t.Fatalf("UpsertFieldCatalog() error = %v", err)
	}
	if err := store.UpsertIssueBundle(ctx, sampleIssue(), ContextConfig{Name: "default", Project: "DEMO"}); err != nil {
		t.Fatalf("UpsertIssueBundle() error = %v", err)
	}
	if err := store.UpsertIssueBundle(ctx, sampleIssueTwo(), ContextConfig{Name: "default", Project: "PLAT"}); err != nil {
		t.Fatalf("UpsertIssueBundle() error = %v", err)
	}

	results, err := store.SearchIssuesByJQL(ctx, `labels in (cli, urgent) AND updated >= "2026-04-22 00:00" ORDER BY updated DESC`, 10)
	if err != nil {
		t.Fatalf("SearchIssuesByJQL() error = %v", err)
	}
	gotKeys := []string{}
	for _, item := range results {
		gotKeys = append(gotKeys, item.Key)
	}
	wantKeys := []string{"PLAT-7", "DEMO-1"}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("SearchIssuesByJQL() keys = %v, want %v", gotKeys, wantKeys)
	}
}

func TestStoreSearchIssuesByJQLReturnsUnsupportedForUnsupportedSyntax(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "jirax.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()
	if err := store.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if err := store.UpsertIssueBundle(ctx, sampleIssue(), ContextConfig{Name: "default", Project: "DEMO"}); err != nil {
		t.Fatalf("UpsertIssueBundle() error = %v", err)
	}

	_, err = store.SearchIssuesByJQL(ctx, `assignee = currentUser()`, 10)
	if err == nil {
		t.Fatal("expected unsupported local JQL error")
	}
	if !strings.Contains(err.Error(), "unsupported local JQL") {
		t.Fatalf("expected unsupported local JQL error, got %v", err)
	}
}

func sampleIssue() JiraIssue {
	return JiraIssue{
		ID:  "10001",
		Key: "DEMO-1",
		Fields: map[string]any{
			"summary":           "Sync local-first Jira issues",
			"description":       "Keep reads fast and local.",
			"status":            map[string]any{"name": "In Progress"},
			"issuetype":         map[string]any{"name": "Task"},
			"priority":          map[string]any{"name": "High"},
			"assignee":          map[string]any{"displayName": "Aleksei"},
			"reporter":          map[string]any{"displayName": "Taylor"},
			"project":           map[string]any{"key": "DEMO"},
			"created":           "2026-04-22T10:00:00.000+0000",
			"updated":           "2026-04-22T12:00:00.000+0000",
			"labels":            []any{"cli", "local-first"},
			"customfield_10010": "Sprint 4",
			"comment": map[string]any{
				"comments": []any{
					map[string]any{
						"id":      "c1",
						"author":  map[string]any{"displayName": "Sam"},
						"body":    "Looks good to me.",
						"created": "2026-04-22T11:00:00.000+0000",
						"updated": "2026-04-22T11:05:00.000+0000",
					},
				},
			},
		},
		Changelog: JiraChangelog{
			Histories: []JiraHistory{
				{
					ID:      "h1",
					Author:  JiraUser{DisplayName: "Sam"},
					Created: "2026-04-22T11:30:00.000+0000",
					Items: []JiraItem{
						{Field: "status", FieldID: "status", FromString: "To Do", ToString: "In Progress"},
					},
				},
			},
		},
	}
}

func sampleIssueTwo() JiraIssue {
	return JiraIssue{
		ID:  "10002",
		Key: "PLAT-7",
		Fields: map[string]any{
			"summary":           "Harden sync retries for enterprise Jira",
			"description":       "Handle proxy timeouts and rate limiting cleanly.",
			"status":            map[string]any{"name": "To Do"},
			"issuetype":         map[string]any{"name": "Bug"},
			"priority":          map[string]any{"name": "Medium"},
			"assignee":          map[string]any{"displayName": "Jordan"},
			"reporter":          map[string]any{"displayName": "Taylor"},
			"project":           map[string]any{"key": "PLAT"},
			"created":           "2026-04-23T08:00:00.000+0000",
			"updated":           "2026-04-23T09:30:00.000+0000",
			"labels":            []any{"cli", "urgent"},
			"customfield_10010": "Sprint 5",
		},
	}
}
