package jirax

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestValidateCreateFields(t *testing.T) {
	meta := map[string]MetaFieldDef{
		"summary": {Required: true},
		"project": {Required: true},
		"labels":  {Required: false},
	}

	errs := ValidateCreateFields(meta, map[string]any{
		"summary": "Ship it",
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errs), errs)
	}
}

func TestValidateEditFields(t *testing.T) {
	meta := map[string]MetaFieldDef{
		"summary": {},
		"labels":  {},
	}

	errs := ValidateEditFields(meta, map[string]any{
		"summary": "Updated",
		"status":  "Done",
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errs), errs)
	}
}

func TestFindTransitionMatchesByNameCaseInsensitive(t *testing.T) {
	got, ok := FindTransition([]Transition{
		{ID: "11", Name: "To Do"},
		{ID: "21", Name: "In Progress"},
		{ID: "31", Name: "Done"},
	}, "done")

	if !ok {
		t.Fatal("expected transition to be found")
	}
	if got.ID != "31" {
		t.Fatalf("expected transition 31, got %+v", got)
	}
}

func TestDoJSONRetriesOnRateLimit(t *testing.T) {
	attempts := 0
	client := &JiraClient{
		baseURL: "https://jira.example.test",
		user:    "u",
		token:   "t",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Status:     "429 Too Many Requests",
						Header:     http.Header{"Retry-After": []string{"0"}},
						Body:       io.NopCloser(strings.NewReader(`{"error":"slow down"}`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"issues":[],"total":0,"startAt":0,"maxResults":50}`)),
				}, nil
			}),
		},
	}

	var resp SearchResponse
	err := client.doJSON(context.Background(), http.MethodPost, "/rest/api/2/search", map[string]any{"jql": "project = DEMO"}, &resp)
	if err != nil {
		t.Fatalf("doJSON() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestSearchIssuesRespectsMaxResults(t *testing.T) {
	attempts := 0
	client := &JiraClient{
		baseURL: "https://jira.example.test",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				var payload struct {
					MaxResults int `json:"maxResults"`
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				if payload.MaxResults != 1 {
					t.Fatalf("expected maxResults=1, got %d", payload.MaxResults)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(bytes.NewBufferString(`{
						"issues":[{"id":"1","key":"DEMO-1","fields":{"summary":"One"}}],
						"total":2,
						"startAt":0,
						"maxResults":1
					}`)),
				}, nil
			}),
		},
	}

	issues, err := client.SearchIssues(context.Background(), SearchOptions{
		JQL:        `project = DEMO`,
		Fields:     []string{"summary"},
		Full:       true,
		MaxResults: 1,
	})
	if err != nil {
		t.Fatalf("SearchIssues() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Key != "DEMO-1" {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	if attempts != 1 {
		t.Fatalf("expected one request, got %d", attempts)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
