package jirax

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type JiraClient struct {
	baseURL    string
	user       string
	token      string
	httpClient *http.Client
	fields     []FieldDefinition
	initErr    error
}

const (
	defaultRetryAttempts = 5
	baseRetryDelay       = 200 * time.Millisecond
	maxRetryDelay        = 30 * time.Second
)

type SearchOptions struct {
	JQL        string
	Fields     []string
	UpdatedAt  time.Time
	Full       bool
	MaxResults int
}

type JiraIssue struct {
	ID        string         `json:"id"`
	Key       string         `json:"key"`
	Self      string         `json:"self"`
	Fields    map[string]any `json:"fields"`
	Changelog JiraChangelog  `json:"changelog"`
}

type JiraChangelog struct {
	Histories []JiraHistory `json:"histories"`
}

type JiraHistory struct {
	ID      string     `json:"id"`
	Author  JiraUser   `json:"author"`
	Created string     `json:"created"`
	Items   []JiraItem `json:"items"`
}

type JiraItem struct {
	Field      string `json:"field"`
	FieldID    string `json:"fieldId"`
	FromString string `json:"fromString"`
	ToString   string `json:"toString"`
}

type JiraUser struct {
	DisplayName string `json:"displayName"`
}

type SearchResponse struct {
	Issues []JiraIssue `json:"issues"`
	Total  int         `json:"total"`
	Start  int         `json:"startAt"`
	Max    int         `json:"maxResults"`
}

type FieldDefinition struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
	Schema struct {
		Type string `json:"type"`
	} `json:"schema"`
}

type IssueView struct {
	Key          string           `json:"key"`
	Summary      string           `json:"summary"`
	Status       string           `json:"status"`
	IssueType    string           `json:"issue_type"`
	Priority     string           `json:"priority,omitempty"`
	Assignee     string           `json:"assignee,omitempty"`
	Reporter     string           `json:"reporter,omitempty"`
	Project      string           `json:"project,omitempty"`
	CreatedAt    string           `json:"created_at,omitempty"`
	UpdatedAt    string           `json:"updated_at,omitempty"`
	Description  string           `json:"description,omitempty"`
	Labels       []string         `json:"labels,omitempty"`
	CustomFields map[string]any   `json:"custom_fields,omitempty"`
	Comments     []map[string]any `json:"comments,omitempty"`
	Changelog    []map[string]any `json:"changelog,omitempty"`
	Raw          map[string]any   `json:"raw,omitempty"`
}

type CreateMeta struct {
	Projects []struct {
		Key        string `json:"key"`
		IssueTypes []struct {
			Name   string                  `json:"name"`
			Fields map[string]MetaFieldDef `json:"fields"`
		} `json:"issuetypes"`
	} `json:"projects"`
}

type MetaFieldDef struct {
	Required bool `json:"required"`
}

type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func NewJiraClient(cfg *Config) *JiraClient {
	httpClient, err := buildHTTPClient(cfg.Server)
	if err != nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	return &JiraClient{
		baseURL:    strings.TrimRight(cfg.Server.BaseURL, "/"),
		user:       cfg.Server.User,
		token:      cfg.Server.Token,
		httpClient: httpClient,
		initErr:    err,
	}
}

func (j *JiraClient) SearchIssues(ctx context.Context, opts SearchOptions) ([]JiraIssue, error) {
	jql := opts.JQL
	if !opts.Full && !opts.UpdatedAt.IsZero() {
		since := opts.UpdatedAt.Add(-5 * time.Minute).UTC().Format("2006-01-02 15:04")
		jql = fmt.Sprintf("(%s) AND updated >= \"%s\"", opts.JQL, since)
	}

	startAt := 0
	maxResults := 50
	var all []JiraIssue
	for {
		if opts.MaxResults > 0 {
			remaining := opts.MaxResults - len(all)
			if remaining <= 0 {
				break
			}
			if remaining < maxResults {
				maxResults = remaining
			}
		}
		payload := map[string]any{
			"jql":        jql,
			"startAt":    startAt,
			"maxResults": maxResults,
			"fields":     opts.Fields,
			"expand":     []string{"changelog"},
		}
		var resp SearchResponse
		if err := j.doJSON(ctx, http.MethodPost, "/rest/api/2/search", payload, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Issues...)
		if opts.MaxResults > 0 && len(all) >= opts.MaxResults {
			break
		}
		startAt += len(resp.Issues)
		if startAt >= resp.Total || len(resp.Issues) == 0 {
			break
		}
	}
	return all, nil
}

func (j *JiraClient) GetIssue(ctx context.Context, key string) (JiraIssue, error) {
	query := url.Values{}
	query.Set("expand", "changelog")
	path := fmt.Sprintf("/rest/api/2/issue/%s?%s", url.PathEscape(key), query.Encode())
	var issue JiraIssue
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &issue); err != nil {
		return JiraIssue{}, err
	}
	return issue, nil
}

func (j *JiraClient) FieldCatalog(ctx context.Context) ([]FieldDefinition, error) {
	if len(j.fields) > 0 {
		return j.fields, nil
	}
	var fields []FieldDefinition
	if err := j.doJSON(ctx, http.MethodGet, "/rest/api/2/field", nil, &fields); err != nil {
		return nil, err
	}
	j.fields = fields
	return fields, nil
}

func (j *JiraClient) GetCreateMeta(ctx context.Context, project, issueType string) (map[string]MetaFieldDef, error) {
	q := url.Values{}
	q.Set("projectKeys", project)
	q.Set("expand", "projects.issuetypes.fields")
	path := "/rest/api/2/issue/createmeta?" + q.Encode()
	var meta CreateMeta
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &meta); err != nil {
		return nil, err
	}
	for _, p := range meta.Projects {
		if p.Key != project {
			continue
		}
		for _, it := range p.IssueTypes {
			if strings.EqualFold(it.Name, issueType) {
				return it.Fields, nil
			}
		}
	}
	return map[string]MetaFieldDef{}, nil
}

func (j *JiraClient) GetEditMeta(ctx context.Context, key string) (map[string]MetaFieldDef, error) {
	var payload struct {
		Fields map[string]MetaFieldDef `json:"fields"`
	}
	path := fmt.Sprintf("/rest/api/2/issue/%s/editmeta", url.PathEscape(key))
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Fields, nil
}

func (j *JiraClient) CreateIssue(ctx context.Context, fields map[string]any) (*IssueRef, error) {
	payload := map[string]any{"fields": fields}
	var out IssueRef
	if err := j.doJSON(ctx, http.MethodPost, "/rest/api/2/issue", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (j *JiraClient) EditIssue(ctx context.Context, key string, fields map[string]any) error {
	payload := map[string]any{"fields": fields}
	path := fmt.Sprintf("/rest/api/2/issue/%s", url.PathEscape(key))
	return j.doJSON(ctx, http.MethodPut, path, payload, nil)
}

func (j *JiraClient) AddComment(ctx context.Context, key, body string) error {
	path := fmt.Sprintf("/rest/api/2/issue/%s/comment", url.PathEscape(key))
	return j.doJSON(ctx, http.MethodPost, path, map[string]any{"body": body}, nil)
}

func (j *JiraClient) GetTransitions(ctx context.Context, key string) ([]Transition, error) {
	path := fmt.Sprintf("/rest/api/2/issue/%s/transitions", url.PathEscape(key))
	var resp struct {
		Transitions []Transition `json:"transitions"`
	}
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Transitions, nil
}

func (j *JiraClient) TransitionIssue(ctx context.Context, key, id string) error {
	path := fmt.Sprintf("/rest/api/2/issue/%s/transitions", url.PathEscape(key))
	return j.doJSON(ctx, http.MethodPost, path, map[string]any{
		"transition": map[string]any{"id": id},
	}, nil)
}

func (j *JiraClient) doJSON(ctx context.Context, method, path string, body any, dest any) error {
	if j.initErr != nil {
		return j.initErr
	}
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	var lastErr error
	for attempt := 0; attempt < defaultRetryAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, j.baseURL+path, bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
		if j.user != "" || j.token != "" {
			req.SetBasicAuth(j.user, j.token)
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := j.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if !shouldRetryError(err) || attempt == defaultRetryAttempts-1 {
				return err
			}
			if sleepErr := waitRetry(ctx, retryDelay(attempt, "")); sleepErr != nil {
				return sleepErr
			}
			continue
		}

		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if dest == nil || len(data) == 0 {
				return nil
			}
			return json.Unmarshal(data, dest)
		}

		lastErr = fmt.Errorf("jira %s %s failed: %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
		if !shouldRetryStatus(resp.StatusCode) || attempt == defaultRetryAttempts-1 {
			return lastErr
		}
		if sleepErr := waitRetry(ctx, retryDelay(attempt, resp.Header.Get("Retry-After"))); sleepErr != nil {
			return sleepErr
		}
	}
	return lastErr
}

type IssueRef struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func ValidateCreateFields(meta map[string]MetaFieldDef, fields map[string]any) []string {
	var errs []string
	for name, def := range meta {
		if def.Required {
			if _, ok := fields[name]; !ok {
				errs = append(errs, fmt.Sprintf("missing required field %q", name))
			}
		}
	}
	return errs
}

func ValidateEditFields(meta map[string]MetaFieldDef, fields map[string]any) []string {
	var errs []string
	for name := range fields {
		if _, ok := meta[name]; !ok {
			errs = append(errs, fmt.Sprintf("field %q is not editable for this issue", name))
		}
	}
	return errs
}

func FindTransition(transitions []Transition, target string) (Transition, bool) {
	for _, t := range transitions {
		if t.ID == target || strings.EqualFold(t.Name, target) {
			return t, true
		}
	}
	return Transition{}, false
}

func buildHTTPClient(cfg ServerConfig) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	if cfg.CACertFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pemData, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, err
		}
		if ok := pool.AppendCertsFromPEM(pemData); !ok {
			return nil, fmt.Errorf("failed to parse CA certificates from %s", cfg.CACertFile)
		}
		tlsConfig.RootCAs = pool
	}

	transport.TLSClientConfig = tlsConfig
	return &http.Client{
		Timeout:   45 * time.Second,
		Transport: transport,
	}, nil
}

func shouldRetryStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

func shouldRetryError(err error) bool {
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "temporarily unavailable") ||
		strings.Contains(msg, "timeout")
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
			delay := time.Duration(seconds) * time.Second
			if delay > maxRetryDelay {
				return maxRetryDelay
			}
			return delay
		}
		if when, err := http.ParseTime(retryAfter); err == nil {
			delay := time.Until(when)
			if delay > 0 {
				if delay > maxRetryDelay {
					return maxRetryDelay
				}
				return delay
			}
		}
	}

	delay := baseRetryDelay << attempt
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
