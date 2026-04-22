package jirax

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	dbPath string
	db     *sql.DB
}

type FieldInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type,omitempty"`
	Custom bool   `json:"custom"`
}

func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{
		dbPath: path,
		db:     db,
	}
	if err := store.execSQL(context.Background(), "PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Bootstrap() error {
	schema := `
CREATE TABLE IF NOT EXISTS issues (
  issue_id TEXT PRIMARY KEY,
  issue_key TEXT NOT NULL UNIQUE,
  project_key TEXT,
  summary TEXT,
  status TEXT,
  issue_type TEXT,
  priority TEXT,
  assignee TEXT,
  reporter TEXT,
  created_at TEXT,
  updated_at TEXT,
  description TEXT,
  labels_json TEXT,
  custom_json TEXT,
  context_name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS issue_raw (
  issue_id TEXT PRIMARY KEY,
  issue_key TEXT NOT NULL UNIQUE,
  raw_json TEXT NOT NULL,
  synced_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS comments (
  comment_id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL,
  issue_key TEXT NOT NULL,
  author TEXT,
  body TEXT,
  created_at TEXT,
  updated_at TEXT
);
CREATE TABLE IF NOT EXISTS changelog (
  history_id TEXT PRIMARY KEY,
  issue_id TEXT NOT NULL,
  issue_key TEXT NOT NULL,
  author TEXT,
  created_at TEXT,
  items_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS field_catalog (
  field_id TEXT PRIMARY KEY,
  field_name TEXT NOT NULL,
  field_type TEXT,
  is_custom INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS field_aliases (
  alias TEXT PRIMARY KEY,
  field_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS operation_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  operation TEXT NOT NULL,
  issue_key TEXT,
  request_json TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sync_state (
  context_name TEXT PRIMARY KEY,
  last_synced_at TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS issue_fts USING fts5(
  issue_key,
  summary,
  description,
  tokenize='porter unicode61'
);
`
	return s.execSQL(context.Background(), schema)
}

func (s *Store) LastSyncTime(ctx context.Context, contextName string) (time.Time, error) {
	var raw sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT last_synced_at FROM sync_state WHERE context_name = ?", contextName).Scan(&raw)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if !raw.Valid || raw.String == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw.String)
}

func (s *Store) RecordSync(ctx context.Context, contextName string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sync_state(context_name, last_synced_at)
VALUES(?, ?)
ON CONFLICT(context_name) DO UPDATE SET last_synced_at=excluded.last_synced_at;
`, contextName, at.Format(time.RFC3339))
	return err
}

func (s *Store) UpsertFieldCatalog(ctx context.Context, fields []FieldDefinition) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, field := range fields {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO field_catalog(field_id, field_name, field_type, is_custom)
VALUES(?, ?, ?, ?)
ON CONFLICT(field_id) DO UPDATE SET
  field_name=excluded.field_name,
  field_type=excluded.field_type,
  is_custom=excluded.is_custom;
`, field.ID, field.Name, field.Schema.Type, boolInt(field.Custom)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO field_aliases(alias, field_id)
VALUES(?, ?)
ON CONFLICT(alias) DO UPDATE SET field_id=excluded.field_id;
`, slugify(field.Name), field.ID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) UpsertIssueBundle(ctx context.Context, issue JiraIssue, cfg ContextConfig) (err error) {
	issueView, rawJSON, comments, changelog, err := normalizeIssue(issue)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `
INSERT INTO issues(
  issue_id, issue_key, project_key, summary, status, issue_type, priority,
  assignee, reporter, created_at, updated_at, description, labels_json, custom_json, context_name
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(issue_id) DO UPDATE SET
  issue_key=excluded.issue_key,
  project_key=excluded.project_key,
  summary=excluded.summary,
  status=excluded.status,
  issue_type=excluded.issue_type,
  priority=excluded.priority,
  assignee=excluded.assignee,
  reporter=excluded.reporter,
  created_at=excluded.created_at,
  updated_at=excluded.updated_at,
  description=excluded.description,
  labels_json=excluded.labels_json,
  custom_json=excluded.custom_json,
  context_name=excluded.context_name;
`, issue.ID, issue.Key, issueView.Project, issueView.Summary, issueView.Status, issueView.IssueType, issueView.Priority,
		issueView.Assignee, issueView.Reporter, issueView.CreatedAt, issueView.UpdatedAt, issueView.Description,
		mustJSON(issueView.Labels), mustJSON(issueView.CustomFields), cfg.Name); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `
INSERT INTO issue_raw(issue_id, issue_key, raw_json, synced_at)
VALUES(?, ?, ?, ?)
ON CONFLICT(issue_id) DO UPDATE SET
  issue_key=excluded.issue_key,
  raw_json=excluded.raw_json,
  synced_at=excluded.synced_at;
`, issue.ID, issue.Key, rawJSON, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM comments WHERE issue_id = ?`, issue.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM changelog WHERE issue_id = ?`, issue.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM issue_fts WHERE issue_key = ?`, issue.Key); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO issue_fts(issue_key, summary, description) VALUES(?, ?, ?)`, issue.Key, issueView.Summary, issueView.Description); err != nil {
		return err
	}

	for _, comment := range comments {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO comments(comment_id, issue_id, issue_key, author, body, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?);
`, stringValue(comment["id"]), issue.ID, issue.Key, stringValue(comment["author"]), stringValue(comment["body"]),
			stringValue(comment["created_at"]), stringValue(comment["updated_at"])); err != nil {
			return err
		}
	}

	for _, history := range changelog {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO changelog(history_id, issue_id, issue_key, author, created_at, items_json)
VALUES(?, ?, ?, ?, ?, ?);
`, stringValue(history["id"]), issue.ID, issue.Key, stringValue(history["author"]),
			stringValue(history["created_at"]), mustJSON(history["items"])); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) GetIssue(ctx context.Context, key string) (*IssueView, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
  i.issue_key AS key,
  i.summary,
  i.status,
  i.issue_type,
  i.priority,
  i.assignee,
  i.reporter,
  i.project_key AS project,
  i.created_at,
  i.updated_at,
  i.description,
  i.labels_json,
  i.custom_json,
  r.raw_json
FROM issues i
JOIN issue_raw r ON r.issue_id = i.issue_id
WHERE i.issue_key = ?
LIMIT 1;
`, key)

	var issue IssueView
	var labelsJSON, customJSON, rawJSON sql.NullString
	err := row.Scan(
		&issue.Key,
		&issue.Summary,
		&issue.Status,
		&issue.IssueType,
		&issue.Priority,
		&issue.Assignee,
		&issue.Reporter,
		&issue.Project,
		&issue.CreatedAt,
		&issue.UpdatedAt,
		&issue.Description,
		&labelsJSON,
		&customJSON,
		&rawJSON,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("issue %s not found", key)
	}
	if err != nil {
		return nil, err
	}

	decodeJSONValue(labelsJSON.String, &issue.Labels)
	decodeJSONValue(customJSON.String, &issue.CustomFields)
	decodeJSONValue(rawJSON.String, &issue.Raw)

	commentRows, err := s.queryMaps(ctx, `
SELECT comment_id AS id, author, body, created_at, updated_at
FROM comments WHERE issue_key = ? ORDER BY created_at;
`, key)
	if err != nil {
		return nil, err
	}
	issue.Comments = commentRows

	changelogRows, err := s.queryMaps(ctx, `
SELECT history_id AS id, author, created_at, items_json
FROM changelog WHERE issue_key = ? ORDER BY created_at;
`, key)
	if err != nil {
		return nil, err
	}
	for _, row := range changelogRows {
		entry := map[string]any{
			"id":         row["id"],
			"author":     row["author"],
			"created_at": row["created_at"],
		}
		var items any
		decodeJSONValue(stringValue(row["items_json"]), &items)
		entry["items"] = items
		issue.Changelog = append(issue.Changelog, entry)
	}
	return &issue, nil
}

func (s *Store) SearchIssues(ctx context.Context, query string, limit int) ([]IssueView, error) {
	if limit <= 0 {
		limit = 20
	}

	var (
		rows *sql.Rows
		err  error
	)
	if strings.TrimSpace(query) == "" {
		rows, err = s.db.QueryContext(ctx, `
SELECT issue_key, summary, status, issue_type, priority, assignee, reporter, project_key, created_at, updated_at, description
FROM issues
ORDER BY updated_at DESC
LIMIT ?;
`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
SELECT i.issue_key, i.summary, i.status, i.issue_type, i.priority, i.assignee, i.reporter, i.project_key, i.created_at, i.updated_at, i.description
FROM issue_fts f
JOIN issues i ON i.issue_key = f.issue_key
WHERE issue_fts MATCH ?
ORDER BY bm25(issue_fts), i.updated_at DESC
LIMIT ?;
`, ftsQuery(query), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IssueView
	for rows.Next() {
		var issue IssueView
		if err := rows.Scan(
			&issue.Key,
			&issue.Summary,
			&issue.Status,
			&issue.IssueType,
			&issue.Priority,
			&issue.Assignee,
			&issue.Reporter,
			&issue.Project,
			&issue.CreatedAt,
			&issue.UpdatedAt,
			&issue.Description,
		); err != nil {
			return nil, err
		}
		out = append(out, issue)
	}
	return out, rows.Err()
}

func (s *Store) SearchIssuesByJQL(ctx context.Context, jql string, limit int) ([]IssueView, error) {
	if limit <= 0 {
		limit = 20
	}
	query, err := parseLocalJQL(jql)
	if err != nil {
		return nil, err
	}

	aliases, err := s.listFieldAliases(ctx)
	if err != nil {
		return nil, err
	}
	issues, err := s.listIssuesForLocalJQL(ctx)
	if err != nil {
		return nil, err
	}

	filtered := make([]IssueView, 0, len(issues))
	for _, issue := range issues {
		match, err := query.filter.Eval(&issue, aliases)
		if err != nil {
			return nil, err
		}
		if match {
			filtered = append(filtered, issue)
		}
	}
	sortIssuesByLocalJQL(filtered, query.orderBy, aliases)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *Store) LogOperation(ctx context.Context, op, issueKey, reqJSON, status string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO operation_log(operation, issue_key, request_json, status, created_at)
VALUES(?, ?, ?, ?, ?);
`, op, issueKey, reqJSON, status, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) CountIssues(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues;`).Scan(&count)
	return count, err
}

func (s *Store) listIssuesForLocalJQL(ctx context.Context) ([]IssueView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT issue_key, summary, status, issue_type, priority, assignee, reporter, project_key, created_at, updated_at, description, labels_json, custom_json
FROM issues;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IssueView
	for rows.Next() {
		var issue IssueView
		var labelsJSON, customJSON sql.NullString
		if err := rows.Scan(
			&issue.Key,
			&issue.Summary,
			&issue.Status,
			&issue.IssueType,
			&issue.Priority,
			&issue.Assignee,
			&issue.Reporter,
			&issue.Project,
			&issue.CreatedAt,
			&issue.UpdatedAt,
			&issue.Description,
			&labelsJSON,
			&customJSON,
		); err != nil {
			return nil, err
		}
		decodeJSONValue(labelsJSON.String, &issue.Labels)
		decodeJSONValue(customJSON.String, &issue.CustomFields)
		out = append(out, issue)
	}
	return out, rows.Err()
}

func (s *Store) listFieldAliases(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT alias, field_id
FROM field_aliases;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var alias, fieldID string
		if err := rows.Scan(&alias, &fieldID); err != nil {
			return nil, err
		}
		out[normalizeLocalJQLIdentifier(alias)] = fieldID
	}
	return out, rows.Err()
}

func (s *Store) ListFields(ctx context.Context) ([]FieldInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT field_id, field_name, field_type, is_custom
FROM field_catalog
ORDER BY is_custom ASC, field_name ASC;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FieldInfo
	for rows.Next() {
		var field FieldInfo
		var isCustom int
		if err := rows.Scan(&field.ID, &field.Name, &field.Type, &isCustom); err != nil {
			return nil, err
		}
		field.Custom = isCustom == 1
		out = append(out, field)
	}
	return out, rows.Err()
}

func (s *Store) ListDistinctStatuses(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, `SELECT DISTINCT status FROM issues WHERE status <> '' ORDER BY status ASC;`)
}

func (s *Store) ListDistinctIssueTypes(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, `SELECT DISTINCT issue_type FROM issues WHERE issue_type <> '' ORDER BY issue_type ASC;`)
}

func (s *Store) ListDistinctProjects(ctx context.Context) ([]string, error) {
	return s.listDistinctStrings(ctx, `SELECT DISTINCT project_key FROM issues WHERE project_key <> '' ORDER BY project_key ASC;`)
}

func (s *Store) execSQL(ctx context.Context, stmt string) error {
	if strings.TrimSpace(stmt) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, stmt)
	return err
}

func (s *Store) queryMaps(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = normalizeSQLValue(values[i])
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func normalizeIssue(issue JiraIssue) (*IssueView, string, []map[string]any, []map[string]any, error) {
	rawJSON := mustJSON(issue)
	view := &IssueView{
		Key:          issue.Key,
		CustomFields: map[string]any{},
	}

	for key, value := range issue.Fields {
		switch key {
		case "summary":
			view.Summary = stringValue(value)
		case "description":
			view.Description = flattenText(value)
		case "status":
			view.Status = nestedString(value, "name")
		case "issuetype":
			view.IssueType = nestedString(value, "name")
		case "priority":
			view.Priority = nestedString(value, "name")
		case "assignee":
			view.Assignee = nestedString(value, "displayName")
		case "reporter":
			view.Reporter = nestedString(value, "displayName")
		case "project":
			view.Project = nestedString(value, "key")
		case "created":
			view.CreatedAt = stringValue(value)
		case "updated":
			view.UpdatedAt = stringValue(value)
		case "labels":
			view.Labels = stringSlice(value)
		case "comment":
		default:
			if strings.HasPrefix(key, "customfield_") {
				view.CustomFields[key] = value
			}
		}
	}

	var comments []map[string]any
	if commentBlock, ok := issue.Fields["comment"].(map[string]any); ok {
		if rawComments, ok := commentBlock["comments"].([]any); ok {
			for _, raw := range rawComments {
				if m, ok := raw.(map[string]any); ok {
					comments = append(comments, map[string]any{
						"id":         stringValue(m["id"]),
						"author":     nestedString(m["author"], "displayName"),
						"body":       flattenText(m["body"]),
						"created_at": stringValue(m["created"]),
						"updated_at": stringValue(m["updated"]),
					})
				}
			}
		}
	}

	var history []map[string]any
	for _, h := range issue.Changelog.Histories {
		var items []map[string]any
		for _, item := range h.Items {
			items = append(items, map[string]any{
				"field":       item.Field,
				"field_id":    item.FieldID,
				"from_string": item.FromString,
				"to_string":   item.ToString,
			})
		}
		history = append(history, map[string]any{
			"id":         h.ID,
			"author":     h.Author.DisplayName,
			"created_at": h.Created,
			"items":      items,
		})
	}
	return view, rawJSON, comments, history, nil
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func slugify(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	replacer := strings.NewReplacer(" ", "_", "-", "_", "/", "_")
	return replacer.Replace(v)
}

func stringValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		data, _ := json.Marshal(t)
		return string(data)
	}
}

func nestedString(v any, key string) string {
	if m, ok := v.(map[string]any); ok {
		return stringValue(m[key])
	}
	return ""
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, stringValue(item))
	}
	return out
}

func flattenText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case map[string]any:
		if content, ok := t["content"].([]any); ok {
			var parts []string
			for _, item := range content {
				parts = append(parts, flattenText(item))
			}
			return strings.TrimSpace(strings.Join(parts, "\n"))
		}
		if text, ok := t["text"].(string); ok {
			return text
		}
		var parts []string
		for _, value := range t {
			parts = append(parts, flattenText(value))
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case []any:
		var parts []string
		for _, item := range t {
			parts = append(parts, flattenText(item))
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return stringValue(v)
	}
}

func decodeJSONValue(raw string, dest any) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	_ = json.Unmarshal([]byte(raw), dest)
}

func ftsQuery(query string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(query), func(r rune) bool {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= 'A' && r <= 'Z' {
			return false
		}
		if r >= '0' && r <= '9' {
			return false
		}
		return r != '_'
	})
	if len(parts) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ReplaceAll(part, `"`, `""`)
		quoted = append(quoted, `"`+part+`"`)
	}
	return strings.Join(quoted, " ")
}

func normalizeSQLValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	default:
		return t
	}
}

func (s *Store) listDistinctStrings(ctx context.Context, query string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
