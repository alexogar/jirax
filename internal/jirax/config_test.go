package jirax

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestContextConfigAllFieldsDedupesCustomFields(t *testing.T) {
	cfg := ContextConfig{
		Project: "DEMO",
		Fields: FieldMap{
			"summary": "summary",
			"sprint":  "customfield_10010",
			"labels":  "labels",
			"sprint2": "customfield_10010",
		},
	}

	got := cfg.AllFields()
	wantContains := []string{"summary", "labels", "comment", "customfield_10010"}
	for _, field := range wantContains {
		found := false
		for _, item := range got {
			if item == field {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected field %q in %v", field, got)
		}
	}
}

func TestContextConfigScopeJQL(t *testing.T) {
	tests := []struct {
		name string
		cfg  ContextConfig
		want string
	}{
		{name: "project", cfg: ContextConfig{Project: "OPS"}, want: "project = OPS"},
		{name: "projects", cfg: ContextConfig{Projects: []string{"OPS", "PLAT"}}, want: "project in (OPS, PLAT)"},
		{name: "jql", cfg: ContextConfig{JQL: "assignee = currentUser()"}, want: "assignee = currentUser()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ScopeJQL(); got != tt.want {
				t.Fatalf("ScopeJQL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" summary, customfield_10010 ,, labels ")
	want := []string{"summary", "customfield_10010", "labels"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV() = %v, want %v", got, want)
	}
}

func TestParseFieldMapCSV(t *testing.T) {
	got := parseFieldMapCSV("summary,sprint:customfield_10010,epic:customfield_10011")
	want := FieldMap{
		"summary": "summary",
		"sprint":  "customfield_10010",
		"epic":    "customfield_10011",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseFieldMapCSV() = %#v, want %#v", got, want)
	}
}

func TestDiscoverConfigPrefersNearestProjectJSON(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "repo")
	childDir := filepath.Join(projectRoot, "sub", "deep")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, ".jirax.json"), []byte(`{"context":{"project":"DEMO"}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	discovery, err := DiscoverConfig(childDir)
	if err != nil {
		t.Fatalf("DiscoverConfig() error = %v", err)
	}
	if discovery.RootDir != projectRoot {
		t.Fatalf("RootDir = %s, want %s", discovery.RootDir, projectRoot)
	}
	if discovery.ProjectConfig != filepath.Join(projectRoot, ".jirax.json") {
		t.Fatalf("ProjectConfig = %s", discovery.ProjectConfig)
	}
	if discovery.OverlayConfig != filepath.Join(projectRoot, ".jirax", "config.json") {
		t.Fatalf("OverlayConfig = %s", discovery.OverlayConfig)
	}
}

func TestLoadDiscoveredConfigMergesProjectAndOverlay(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".jirax.json"), []byte(`{
  "server": {"base_url":"https://jira.example.com","user":"agent@example.com","ca_cert_file":"/tmp/company-ca.pem"},
  "sync": {"check_interval_minutes":30,"max_staleness_minutes":180},
  "context": {"projects":["OPS","PLAT"],"fields":{"sprint":"customfield_10010"}},
  "aliases": {"sprint":"customfield_10010"}
}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".jirax"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".jirax", "config.json"), []byte(`{
  "context": {"name":"local","project":"DEMO"},
  "server": {"token":"secret"}
}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadDiscoveredConfig(&ConfigDiscovery{
		RootDir:       root,
		ProjectConfig: filepath.Join(root, ".jirax.json"),
		OverlayConfig: filepath.Join(root, ".jirax", "config.json"),
	})
	if err != nil {
		t.Fatalf("LoadDiscoveredConfig() error = %v", err)
	}
	if cfg.Context.Name != "local" || cfg.Context.Project != "DEMO" {
		t.Fatalf("unexpected merged context: %+v", cfg.Context)
	}
	if len(cfg.Context.Projects) != 0 {
		t.Fatalf("expected overlay project to override project list: %+v", cfg.Context)
	}
	if cfg.Server.BaseURL != "https://jira.example.com" || cfg.Server.User != "agent@example.com" || cfg.Server.Token != "secret" {
		t.Fatalf("unexpected merged server config: %+v", cfg.Server)
	}
	if cfg.Server.CACertFile != "/tmp/company-ca.pem" {
		t.Fatalf("unexpected ca cert file: %+v", cfg.Server)
	}
	if cfg.Sync.CheckIntervalMinutes != 30 || cfg.Sync.MaxStalenessMinutes != 180 || !cfg.Sync.AllowStaleOnError {
		t.Fatalf("unexpected sync config: %+v", cfg.Sync)
	}
	if cfg.Aliases["sprint"] != "customfield_10010" {
		t.Fatalf("expected alias to survive merge, got %+v", cfg.Aliases)
	}
}

func TestLoadConfigFromConfFile(t *testing.T) {
	root := t.TempDir()
	confPath := filepath.Join(root, ".jirax.conf")
	content := `
# project-level jirax defaults
base_url=https://jira.example.com
user=agent@example.com
token=top-secret
ca_cert_file=/tmp/company-ca.pem
insecure_skip_verify=true
check_interval_minutes=20
max_staleness_minutes=90
allow_stale_on_error=true
name=repo
project=PLAT
projects=
fields=customfield_10010,customfield_10020
alias.sprint=customfield_10010
`
	if err := os.WriteFile(confPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadConfig(confPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Server.BaseURL != "https://jira.example.com" || cfg.Context.Project != "PLAT" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Server.CACertFile != "/tmp/company-ca.pem" || !cfg.Server.InsecureSkipVerify {
		t.Fatalf("unexpected tls config: %+v", cfg.Server)
	}
	if cfg.Sync.CheckIntervalMinutes != 20 || cfg.Sync.MaxStalenessMinutes != 90 || !cfg.Sync.AllowStaleOnError {
		t.Fatalf("unexpected sync config: %+v", cfg.Sync)
	}
	if !reflect.DeepEqual(cfg.Context.Fields, FieldMap{
		"customfield_10010": "customfield_10010",
		"customfield_10020": "customfield_10020",
	}) {
		t.Fatalf("unexpected fields: %+v", cfg.Context.Fields)
	}
	if cfg.Aliases["sprint"] != "customfield_10010" {
		t.Fatalf("unexpected aliases: %+v", cfg.Aliases)
	}
}

func TestLoadConfigFromConfFileWithProjects(t *testing.T) {
	root := t.TempDir()
	confPath := filepath.Join(root, ".jirax.conf")
	content := `
base_url=https://jira.example.com
projects=OPS,PLAT,CORE
fields=sprint:customfield_10010
`
	if err := os.WriteFile(confPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadConfig(confPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !reflect.DeepEqual(cfg.Context.Projects, []string{"OPS", "PLAT", "CORE"}) {
		t.Fatalf("unexpected projects: %+v", cfg.Context.Projects)
	}
	if cfg.Context.Project != "" {
		t.Fatalf("expected single project to be empty: %+v", cfg.Context)
	}
}

func TestApplyEnvFallbacksSetsSyncDefaults(t *testing.T) {
	cfg := &Config{}
	if err := cfg.ApplyEnvFallbacks(); err != nil {
		t.Fatalf("ApplyEnvFallbacks() error = %v", err)
	}
	if cfg.Sync.CheckIntervalMinutes != 15 || cfg.Sync.MaxStalenessMinutes != 240 || !cfg.Sync.AllowStaleOnError {
		t.Fatalf("unexpected sync defaults: %+v", cfg.Sync)
	}
}
