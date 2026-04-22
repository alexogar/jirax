package jirax

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Server       ServerConfig      `json:"server"`
	Context      ContextConfig     `json:"context"`
	DatabasePath string            `json:"database_path"`
	Aliases      map[string]string `json:"aliases"`
}

type ServerConfig struct {
	BaseURL            string `json:"base_url"`
	User               string `json:"user"`
	Token              string `json:"token"`
	CACertFile         string `json:"ca_cert_file,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
}

type ContextConfig struct {
	Name     string   `json:"name"`
	Project  string   `json:"project,omitempty"`
	Projects []string `json:"projects,omitempty"`
	JQL      string   `json:"jql,omitempty"`
	Fields   FieldMap `json:"fields,omitempty"`
}

type FieldMap map[string]string

type ConfigDiscovery struct {
	RootDir       string
	ProjectConfig string
	OverlayConfig string
}

func DiscoverConfig(startDir string) (*ConfigDiscovery, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	current, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}

	for {
		projectJSON := filepath.Join(current, ".jirax.json")
		projectConf := filepath.Join(current, ".jirax.conf")
		overlay := filepath.Join(current, ".jirax", "config.json")

		hasJSON := fileExists(projectJSON)
		hasConf := fileExists(projectConf)
		hasOverlay := fileExists(overlay)
		if hasJSON || hasConf || hasOverlay {
			projectConfig := ""
			if hasJSON {
				projectConfig = projectJSON
			} else if hasConf {
				projectConfig = projectConf
			}
			return &ConfigDiscovery{
				RootDir:       current,
				ProjectConfig: projectConfig,
				OverlayConfig: overlay,
			}, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return &ConfigDiscovery{
		RootDir:       startDir,
		ProjectConfig: "",
		OverlayConfig: filepath.Join(startDir, ".jirax", "config.json"),
	}, nil
}

func LoadDiscoveredConfig(discovery *ConfigDiscovery) (*Config, error) {
	cfg := defaultConfig()

	if discovery != nil && discovery.ProjectConfig != "" {
		if err := mergeConfigFile(cfg, discovery.ProjectConfig); err != nil {
			return nil, err
		}
	}
	if discovery != nil && fileExists(discovery.OverlayConfig) {
		if err := mergeConfigFile(cfg, discovery.OverlayConfig); err != nil {
			return nil, err
		}
	}
	if err := cfg.ApplyEnvFallbacks(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()
	if !fileExists(path) {
		if err := cfg.ApplyEnvFallbacks(); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err := mergeConfigFile(cfg, path); err != nil {
		return nil, err
	}
	if err := cfg.ApplyEnvFallbacks(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = defaultDBPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func defaultConfig() *Config {
	return &Config{
		Context: ContextConfig{
			Name: "default",
		},
		DatabasePath: defaultDBPath(),
		Aliases:      map[string]string{},
	}
}

func defaultDBPath() string {
	if v := strings.TrimSpace(os.Getenv("JIRAX_DB_PATH")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err == nil {
		candidate := filepath.Join(home, ".local", "share", "jirax", "jirax.db")
		if canUseDir(filepath.Dir(candidate)) {
			return candidate
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "jirax.db"
	}
	return filepath.Join(cwd, ".jirax", "jirax.db")
}

func (c *Config) ApplyEnvFallbacks() error {
	if c.DatabasePath == "" {
		c.DatabasePath = defaultDBPath()
	}
	if c.Server.BaseURL == "" {
		c.Server.BaseURL = os.Getenv("JIRAX_BASE_URL")
	}
	if c.Server.User == "" {
		c.Server.User = os.Getenv("JIRAX_USER")
	}
	if c.Server.Token == "" {
		c.Server.Token = os.Getenv("JIRAX_TOKEN")
	}
	if c.Server.CACertFile == "" {
		c.Server.CACertFile = os.Getenv("JIRAX_CA_CERT_FILE")
	}
	if !c.Server.InsecureSkipVerify {
		c.Server.InsecureSkipVerify = envBool("JIRAX_INSECURE_SKIP_VERIFY")
	}
	if c.Context.Name == "" {
		c.Context.Name = "default"
	}
	if c.Aliases == nil {
		c.Aliases = map[string]string{}
	}
	return nil
}

func (c *Config) Clone() *Config {
	cp := *c
	cp.Context.Projects = append([]string(nil), c.Context.Projects...)
	cp.Context.Fields = c.Context.Fields.Clone()
	cp.Aliases = map[string]string{}
	for k, v := range c.Aliases {
		cp.Aliases[k] = v
	}
	return &cp
}

func (c *Config) ValidateContext() error {
	projectCount := 0
	if c.Context.Project != "" {
		projectCount++
	}
	if len(c.Context.Projects) > 0 {
		projectCount++
	}
	if c.Context.JQL != "" {
		projectCount++
	}
	if projectCount == 0 {
		return errors.New("context missing scope; use `jirax ctx set --project KEY`, `--projects A,B`, or `--jql QUERY`")
	}
	if projectCount > 1 {
		return errors.New("context can only use one of project, projects, or jql")
	}
	return nil
}

func (c *Config) HasServer() bool {
	return strings.TrimSpace(c.Server.BaseURL) != "" &&
		strings.TrimSpace(c.Server.User) != "" &&
		strings.TrimSpace(c.Server.Token) != ""
}

func (c ContextConfig) ScopeJQL() string {
	if c.JQL != "" {
		return c.JQL
	}
	if len(c.Projects) > 0 {
		quoted := make([]string, 0, len(c.Projects))
		for _, project := range c.Projects {
			project = strings.TrimSpace(project)
			if project == "" {
				continue
			}
			quoted = append(quoted, project)
		}
		return "project in (" + strings.Join(quoted, ", ") + ")"
	}
	return "project = " + c.Project
}

func (c ContextConfig) AllFields() []string {
	fields := []string{
		"summary", "description", "status", "issuetype", "assignee", "reporter",
		"priority", "updated", "created", "labels", "comment",
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(fields)+len(c.Fields))
	for _, field := range fields {
		if !seen[field] {
			seen[field] = true
			out = append(out, field)
		}
	}
	for _, field := range c.Fields.Values() {
		if !seen[field] {
			seen[field] = true
			out = append(out, field)
		}
	}
	return out
}

func canUseDir(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	testFile := filepath.Join(dir, ".jirax-write-test")
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return false
	}
	_ = os.Remove(testFile)
	return true
}

func mergeConfigFile(cfg *Config, path string) error {
	if strings.HasSuffix(path, ".conf") {
		return mergeConfConfig(cfg, path)
	}
	return mergeJSONConfig(cfg, path)
}

func mergeJSONConfig(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var incoming Config
	if err := json.Unmarshal(data, &incoming); err != nil {
		return err
	}
	mergeConfig(cfg, &incoming)
	return nil
}

func mergeConfConfig(cfg *Config, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid config line in %s: %q", path, line)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch key {
		case "base_url":
			cfg.Server.BaseURL = value
		case "user":
			cfg.Server.User = value
		case "token":
			cfg.Server.Token = value
		case "ca_cert_file":
			cfg.Server.CACertFile = value
		case "insecure_skip_verify":
			cfg.Server.InsecureSkipVerify = strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes")
		case "database_path":
			cfg.DatabasePath = value
		case "name":
			cfg.Context.Name = value
		case "project":
			cfg.Context.Project = value
			cfg.Context.Projects = nil
		case "projects":
			cfg.Context.Projects = splitCSV(value)
			if len(cfg.Context.Projects) > 0 {
				cfg.Context.Project = ""
			}
		case "jql":
			cfg.Context.JQL = value
		case "fields":
			cfg.Context.Fields = parseFieldMapCSV(value)
		default:
			if strings.HasPrefix(key, "alias.") {
				if cfg.Aliases == nil {
					cfg.Aliases = map[string]string{}
				}
				cfg.Aliases[strings.TrimPrefix(key, "alias.")] = value
				continue
			}
			return fmt.Errorf("unknown config key in %s: %s", path, key)
		}
	}
	return scanner.Err()
}

func mergeConfig(dst, src *Config) {
	if src.Server.BaseURL != "" {
		dst.Server.BaseURL = src.Server.BaseURL
	}
	if src.Server.User != "" {
		dst.Server.User = src.Server.User
	}
	if src.Server.Token != "" {
		dst.Server.Token = src.Server.Token
	}
	if src.Server.CACertFile != "" {
		dst.Server.CACertFile = src.Server.CACertFile
	}
	if src.Server.InsecureSkipVerify {
		dst.Server.InsecureSkipVerify = true
	}
	if src.Context.Name != "" {
		dst.Context.Name = src.Context.Name
	}
	if src.Context.Project != "" {
		dst.Context.Project = src.Context.Project
		dst.Context.Projects = nil
	}
	if len(src.Context.Projects) > 0 {
		dst.Context.Projects = append([]string(nil), src.Context.Projects...)
		dst.Context.Project = ""
	}
	if src.Context.JQL != "" {
		dst.Context.JQL = src.Context.JQL
	}
	if len(src.Context.Fields) > 0 {
		dst.Context.Fields = src.Context.Fields.Clone()
	}
	if src.DatabasePath != "" {
		dst.DatabasePath = src.DatabasePath
	}
	if dst.Aliases == nil {
		dst.Aliases = map[string]string{}
	}
	for k, v := range src.Aliases {
		dst.Aliases[k] = v
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func envBool(key string) bool {
	value := strings.TrimSpace(os.Getenv(key))
	return strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes")
}

func (f FieldMap) Clone() FieldMap {
	if len(f) == 0 {
		return nil
	}
	out := make(FieldMap, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

func (f FieldMap) Values() []string {
	if len(f) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(f))
	for _, value := range f {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (f *FieldMap) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = nil
		return nil
	}

	var asMap map[string]string
	if err := json.Unmarshal(data, &asMap); err == nil {
		out := FieldMap{}
		for k, v := range asMap {
			key := strings.TrimSpace(k)
			value := strings.TrimSpace(v)
			if key == "" {
				continue
			}
			if value == "" {
				value = key
			}
			out[key] = value
		}
		*f = out
		return nil
	}

	var asList []string
	if err := json.Unmarshal(data, &asList); err == nil {
		*f = parseFieldMapCSV(strings.Join(asList, ","))
		return nil
	}

	return fmt.Errorf("fields must be an object or array")
}

func parseFieldMapCSV(v string) FieldMap {
	parts := splitCSV(v)
	if len(parts) == 0 {
		return nil
	}
	out := FieldMap{}
	for _, part := range parts {
		name := part
		value := part
		if strings.Contains(part, ":") {
			pair := strings.SplitN(part, ":", 2)
			name = strings.TrimSpace(pair[0])
			value = strings.TrimSpace(pair[1])
		}
		if name == "" {
			continue
		}
		if value == "" {
			value = name
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
