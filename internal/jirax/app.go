package jirax

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type App struct {
	configPath string
	config     *Config
	store      *Store
	jira       *JiraClient
	discovery  *ConfigDiscovery
	now        func() time.Time
}

type UsageError struct {
	Message string
}

func (e *UsageError) Error() string {
	return e.Message
}

func NewApp() (*App, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	discovery, err := DiscoverConfig(cwd)
	if err != nil {
		return nil, err
	}
	cfg, err := LoadDiscoveredConfig(discovery)
	if err != nil {
		return nil, err
	}

	app := &App{
		configPath: discovery.OverlayConfig,
		config:     cfg,
		discovery:  discovery,
		now:        time.Now,
	}
	if cfg.HasServer() {
		app.jira = NewJiraClient(cfg)
	}
	return app, nil
}

func (a *App) ensureStore() error {
	if a.store != nil {
		return nil
	}
	store, err := NewStore(a.config.DatabasePath)
	if err != nil {
		fallback := filepath.Join(".", ".jirax", "jirax.db")
		if a.config.DatabasePath == fallback {
			return err
		}
		store, err = NewStore(fallback)
		if err != nil {
			return err
		}
		a.config.DatabasePath = fallback
	}
	if err := store.Bootstrap(); err != nil {
		return err
	}
	a.store = store
	return nil
}

func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return &UsageError{Message: usageText()}
	}

	switch args[0] {
	case "config":
		return a.runConfig(ctx, args[1:])
	case "ctx":
		return a.runContext(ctx, args[1:])
	case "know", "knowledge":
		return a.runKnowledge(ctx, args[1:])
	case "sync":
		return a.runSync(ctx, args[1:])
	case "issue":
		return a.runIssue(ctx, args[1:])
	case "search":
		return a.runSearch(ctx, args[1:])
	case "create":
		return a.runCreate(ctx, args[1:])
	case "edit":
		return a.runEdit(ctx, args[1:])
	case "comment":
		return a.runComment(ctx, args[1:])
	case "transition":
		return a.runTransition(ctx, args[1:])
	case "help", "--help", "-h":
		fmt.Println(usageText())
		return nil
	default:
		return &UsageError{Message: usageText()}
	}
}

func (a *App) runContext(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "set" {
		return &UsageError{Message: "usage: jirax ctx set --project KEY | --jql QUERY [options]"}
	}

	fs := flag.NewFlagSet("ctx set", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	baseURL := fs.String("base-url", "", "Jira base URL")
	user := fs.String("user", "", "Jira username/email")
	token := fs.String("token", "", "Jira API token/password")
	caCertFile := fs.String("ca-cert-file", "", "PEM file for internal/custom Jira CA")
	insecureSkipVerify := fs.Bool("insecure-skip-verify", false, "Skip TLS verification for internal Jira only")
	project := fs.String("project", "", "Project key scope")
	projectsCSV := fs.String("projects", "", "Comma-separated project key scope")
	jql := fs.String("jql", "", "JQL scope")
	name := fs.String("name", "default", "Context name")
	fieldsCSV := fs.String("fields", "", "Comma-separated additional fields")
	syncCheckMinutes := fs.Int("sync-check-minutes", 0, "How often Jirax should check for remote updates before read commands")
	syncMaxStaleMinutes := fs.Int("sync-max-stale-minutes", 0, "Maximum cache age before Jirax auto-syncs")
	outputJSON := fs.Bool("json", false, "JSON output")

	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	selected := 0
	if strings.TrimSpace(*project) != "" {
		selected++
	}
	if strings.TrimSpace(*projectsCSV) != "" {
		selected++
	}
	if strings.TrimSpace(*jql) != "" {
		selected++
	}
	if selected != 1 {
		return errors.New("choose exactly one of --project, --projects, or --jql")
	}

	cfg := a.config.Clone()
	cfg.Context.Name = *name
	cfg.Context.Project = strings.TrimSpace(*project)
	cfg.Context.Projects = splitCSV(*projectsCSV)
	cfg.Context.JQL = strings.TrimSpace(*jql)
	if len(cfg.Context.Projects) > 0 {
		cfg.Context.Project = ""
	}
	if cfg.Context.JQL != "" {
		cfg.Context.Project = ""
		cfg.Context.Projects = nil
	}
	cfg.Context.Fields = parseFieldMapCSV(*fieldsCSV)
	if *syncCheckMinutes > 0 {
		cfg.Sync.CheckIntervalMinutes = *syncCheckMinutes
	}
	if *syncMaxStaleMinutes > 0 {
		cfg.Sync.MaxStalenessMinutes = *syncMaxStaleMinutes
	}
	if *baseURL != "" {
		cfg.Server.BaseURL = *baseURL
	}
	if *user != "" {
		cfg.Server.User = *user
	}
	if *token != "" {
		cfg.Server.Token = *token
	}
	if *caCertFile != "" {
		cfg.Server.CACertFile = *caCertFile
	}
	if *insecureSkipVerify {
		cfg.Server.InsecureSkipVerify = true
	}
	if err := cfg.ApplyEnvFallbacks(); err != nil {
		return err
	}
	if err := cfg.ValidateContext(); err != nil {
		return err
	}
	if err := SaveConfig(a.configPath, cfg); err != nil {
		return err
	}

	a.config = cfg
	a.jira = NewJiraClient(cfg)

	if *outputJSON {
		return printJSON(map[string]any{
			"context": cfg.Context,
			"db_path": cfg.DatabasePath,
		})
	}

	fmt.Printf("context %q saved\n", cfg.Context.Name)
	fmt.Printf("scope: %s\n", cfg.Context.ScopeJQL())
	fmt.Printf("db: %s\n", cfg.DatabasePath)
	return nil
}

func (a *App) runSync(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outputJSON := fs.Bool("json", false, "JSON output")
	full := fs.Bool("full", false, "Ignore incremental cursor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := a.requireConfigured(); err != nil {
		return err
	}

	result, err := a.syncIssues(ctx, SyncOptions{Full: *full})
	if err != nil {
		return err
	}
	if *outputJSON {
		return printJSON(result)
	}
	fmt.Printf("synced %d issues (%d changed) at %s\n", result.Scanned, result.Changed, result.FinishedAt.Format(time.RFC3339))
	return nil
}

func (a *App) runConfig(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "show" {
		return &UsageError{Message: "usage: jirax config show [--json]"}
	}
	fs := flag.NewFlagSet("config show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outputJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	payload := map[string]any{
		"config": a.config,
		"discovery": map[string]any{
			"root_dir":       "",
			"project_config": "",
			"overlay_config": a.configPath,
		},
	}
	if a.discovery != nil {
		payload["discovery"] = map[string]any{
			"root_dir":       a.discovery.RootDir,
			"project_config": a.discovery.ProjectConfig,
			"overlay_config": a.discovery.OverlayConfig,
		}
	}
	if *outputJSON {
		return printJSON(payload)
	}

	fmt.Printf("root: %s\n", a.discovery.RootDir)
	if a.discovery.ProjectConfig != "" {
		fmt.Printf("project config: %s\n", a.discovery.ProjectConfig)
	}
	fmt.Printf("overlay config: %s\n", a.configPath)
	fmt.Printf("scope: %s\n", a.config.Context.ScopeJQL())
	if len(a.config.Context.Fields) > 0 {
		fmt.Printf("fields: %s\n", strings.Join(a.config.Context.AllFields(), ", "))
	}
	if len(a.config.Context.Projects) > 0 {
		fmt.Printf("projects: %s\n", strings.Join(a.config.Context.Projects, ", "))
	}
	return nil
}

func (a *App) runKnowledge(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("know", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outputJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureStore(); err != nil {
		return err
	}
	if err := a.syncIfNeeded(ctx); err != nil {
		return err
	}

	subcommand := "overview"
	if fs.NArg() > 0 {
		subcommand = fs.Arg(0)
	}

	switch subcommand {
	case "overview":
		return a.runKnowledgeOverview(ctx, *outputJSON)
	case "fields":
		return a.runKnowledgeFields(ctx, *outputJSON)
	case "statuses":
		return a.runKnowledgeStatuses(ctx, *outputJSON)
	case "types":
		return a.runKnowledgeTypes(ctx, *outputJSON)
	case "transitions":
		if fs.NArg() < 2 {
			return &UsageError{Message: "usage: jirax know transitions ISSUE-123 [--json]"}
		}
		return a.runKnowledgeTransitions(ctx, fs.Arg(1), *outputJSON)
	default:
		return &UsageError{Message: "usage: jirax know [overview|fields|statuses|types|transitions ISSUE-123] [--json]"}
	}
}

func (a *App) runKnowledgeOverview(ctx context.Context, outputJSON bool) error {
	count, err := a.store.CountIssues(ctx)
	if err != nil {
		return err
	}
	lastSync, err := a.store.LastSyncTime(ctx, a.config.Context.Name)
	if err != nil {
		return err
	}
	fields, err := a.store.ListFields(ctx)
	if err != nil {
		return err
	}
	statuses, err := a.store.ListDistinctStatuses(ctx)
	if err != nil {
		return err
	}
	types, err := a.store.ListDistinctIssueTypes(ctx)
	if err != nil {
		return err
	}
	projects, err := a.store.ListDistinctProjects(ctx)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"context": map[string]any{
			"name":     a.config.Context.Name,
			"scope":    a.config.Context.ScopeJQL(),
			"project":  a.config.Context.Project,
			"projects": a.config.Context.Projects,
			"jql":      a.config.Context.JQL,
			"fields":   a.config.Context.AllFields(),
		},
		"database_path": a.config.DatabasePath,
		"issue_count":   count,
		"last_sync_at":  lastSync.Format(time.RFC3339),
		"projects":      projects,
		"statuses":      statuses,
		"issue_types":   types,
		"field_count":   len(fields),
	}
	if outputJSON {
		return printJSON(payload)
	}

	fmt.Printf("context: %s\n", a.config.Context.Name)
	fmt.Printf("scope: %s\n", a.config.Context.ScopeJQL())
	fmt.Printf("issues: %d\n", count)
	if !lastSync.IsZero() {
		fmt.Printf("last sync: %s\n", lastSync.Format(time.RFC3339))
	}
	if len(projects) > 0 {
		fmt.Printf("projects: %s\n", strings.Join(projects, ", "))
	}
	if len(statuses) > 0 {
		fmt.Printf("statuses: %s\n", strings.Join(statuses, ", "))
	}
	if len(types) > 0 {
		fmt.Printf("types: %s\n", strings.Join(types, ", "))
	}
	fmt.Printf("fields cached: %d\n", len(fields))
	return nil
}

func (a *App) runKnowledgeFields(ctx context.Context, outputJSON bool) error {
	fields, err := a.store.ListFields(ctx)
	if err != nil {
		return err
	}
	if outputJSON {
		return printJSON(map[string]any{"fields": fields})
	}
	for _, field := range fields {
		kind := "core"
		if field.Custom {
			kind = "custom"
		}
		if field.Type != "" {
			fmt.Printf("%-18s %-8s %-20s %s\n", field.ID, kind, field.Type, field.Name)
			continue
		}
		fmt.Printf("%-18s %-8s %s\n", field.ID, kind, field.Name)
	}
	return nil
}

func (a *App) runKnowledgeStatuses(ctx context.Context, outputJSON bool) error {
	statuses, err := a.store.ListDistinctStatuses(ctx)
	if err != nil {
		return err
	}
	if outputJSON {
		return printJSON(map[string]any{"statuses": statuses})
	}
	for _, status := range statuses {
		fmt.Println(status)
	}
	return nil
}

func (a *App) runKnowledgeTypes(ctx context.Context, outputJSON bool) error {
	types, err := a.store.ListDistinctIssueTypes(ctx)
	if err != nil {
		return err
	}
	if outputJSON {
		return printJSON(map[string]any{"issue_types": types})
	}
	for _, issueType := range types {
		fmt.Println(issueType)
	}
	return nil
}

func (a *App) runKnowledgeTransitions(ctx context.Context, key string, outputJSON bool) error {
	if err := a.syncIssueByKey(ctx, key); err != nil {
		return err
	}
	transitions, err := a.jira.GetTransitions(ctx, key)
	if err != nil {
		return err
	}
	if outputJSON {
		return printJSON(map[string]any{"issue": key, "transitions": transitions})
	}
	fmt.Printf("transitions for %s\n", key)
	for _, transition := range transitions {
		fmt.Printf("%-8s %s\n", transition.ID, transition.Name)
	}
	return nil
}

func (a *App) runIssue(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("issue", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outputJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &UsageError{Message: "usage: jirax issue ISSUE-123 [--json]"}
	}
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureStore(); err != nil {
		return err
	}
	key := fs.Arg(0)
	if err := a.syncIssueByKey(ctx, key); err != nil {
		return err
	}

	issue, err := a.store.GetIssue(ctx, key)
	if err != nil {
		return err
	}
	if *outputJSON {
		return printJSON(issue)
	}
	printIssue(issue)
	return nil
}

func (a *App) runSearch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outputJSON := fs.Bool("json", false, "JSON output")
	limit := fs.Int("limit", 20, "Result limit")
	jql := fs.String("jql", "", "JQL query to run against local cache, Jira, or both")
	mode := fs.String("mode", "auto", "Search mode: text, local, remote, auto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureStore(); err != nil {
		return err
	}
	if err := a.syncIfNeeded(ctx); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))

	results, resolvedMode, resolvedJQL, err := a.searchIssues(ctx, query, *jql, *mode, *limit)
	if err != nil {
		return err
	}
	if *outputJSON {
		return printJSON(map[string]any{
			"results": results,
			"mode":    resolvedMode,
			"jql":     resolvedJQL,
		})
	}
	if resolvedJQL != "" {
		fmt.Printf("mode: %s\n", resolvedMode)
		fmt.Printf("jql: %s\n", resolvedJQL)
	}
	for _, issue := range results {
		fmt.Printf("%-12s %-12s %s\n", issue.Key, issue.Status, issue.Summary)
	}
	return nil
}

func (a *App) runCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	project := fs.String("project", "", "Project key")
	issueType := fs.String("type", "Task", "Issue type name")
	summary := fs.String("summary", "", "Issue summary")
	description := fs.String("description", "", "Issue description")
	fieldsJSON := fs.String("fields-json", "", "JSON object with extra fields")
	dryRun := fs.Bool("dry-run", false, "Validate only")
	outputJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureStore(); err != nil {
		return err
	}
	if err := a.syncIfNeeded(ctx); err != nil {
		return err
	}
	if *project == "" {
		switch {
		case a.config.Context.Project != "":
			*project = a.config.Context.Project
		case len(a.config.Context.Projects) == 1:
			*project = a.config.Context.Projects[0]
		}
	}
	if *project == "" || *summary == "" {
		return errors.New("--project and --summary are required; multi-project contexts must choose an explicit --project")
	}

	fields, err := parseJSONObject(*fieldsJSON)
	if err != nil {
		return err
	}
	fields["project"] = map[string]any{"key": *project}
	fields["issuetype"] = map[string]any{"name": *issueType}
	fields["summary"] = *summary
	if *description != "" {
		fields["description"] = *description
	}

	meta, err := a.jira.GetCreateMeta(ctx, *project, *issueType)
	if err != nil {
		return err
	}
	validation := ValidateCreateFields(meta, fields)
	if *dryRun {
		payload := map[string]any{"valid": len(validation) == 0, "errors": validation, "fields": fields}
		if *outputJSON {
			return printJSON(payload)
		}
		if len(validation) == 0 {
			fmt.Println("create validation passed")
			return nil
		}
		return errors.New(strings.Join(validation, "; "))
	}
	if len(validation) > 0 {
		return errors.New(strings.Join(validation, "; "))
	}

	created, err := a.jira.CreateIssue(ctx, fields)
	if err != nil {
		return err
	}
	if err := a.store.LogOperation(ctx, "create", created.Key, mustJSON(fields), "ok"); err != nil {
		return err
	}
	if err := a.syncIssueByKey(ctx, created.Key); err != nil {
		return err
	}
	if *outputJSON {
		return printJSON(created)
	}
	fmt.Printf("created %s\n", created.Key)
	return nil
}

func (a *App) runEdit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fieldsJSON := fs.String("fields-json", "", "JSON object with fields to update")
	dryRun := fs.Bool("dry-run", false, "Validate only")
	outputJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &UsageError{Message: "usage: jirax edit ISSUE-123 --fields-json '{\"summary\":\"...\"}' [--dry-run]"}
	}
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureStore(); err != nil {
		return err
	}
	if err := a.syncIfNeeded(ctx); err != nil {
		return err
	}
	key := fs.Arg(0)
	fields, err := parseJSONObject(*fieldsJSON)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return errors.New("no fields provided")
	}

	if err := a.syncIssueByKey(ctx, key); err != nil {
		return err
	}
	meta, err := a.jira.GetEditMeta(ctx, key)
	if err != nil {
		return err
	}
	validation := ValidateEditFields(meta, fields)
	if *dryRun {
		payload := map[string]any{"valid": len(validation) == 0, "errors": validation, "fields": fields}
		if *outputJSON {
			return printJSON(payload)
		}
		if len(validation) == 0 {
			fmt.Println("edit validation passed")
			return nil
		}
		return errors.New(strings.Join(validation, "; "))
	}
	if len(validation) > 0 {
		return errors.New(strings.Join(validation, "; "))
	}

	if err := a.jira.EditIssue(ctx, key, fields); err != nil {
		return err
	}
	if err := a.store.LogOperation(ctx, "edit", key, mustJSON(fields), "ok"); err != nil {
		return err
	}
	if err := a.syncIssueByKey(ctx, key); err != nil {
		return err
	}
	if *outputJSON {
		return printJSON(map[string]any{"updated": key})
	}
	fmt.Printf("updated %s\n", key)
	return nil
}

func (a *App) runComment(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	body := fs.String("body", "", "Comment body")
	dryRun := fs.Bool("dry-run", false, "Validate only")
	outputJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &UsageError{Message: "usage: jirax comment ISSUE-123 --body 'text' [--dry-run]"}
	}
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureStore(); err != nil {
		return err
	}
	if err := a.syncIfNeeded(ctx); err != nil {
		return err
	}
	key := fs.Arg(0)
	if *body == "" {
		return errors.New("--body is required")
	}
	if err := a.syncIssueByKey(ctx, key); err != nil {
		return err
	}
	if *dryRun {
		payload := map[string]any{"valid": true, "issue": key, "body": *body}
		if *outputJSON {
			return printJSON(payload)
		}
		fmt.Println("comment validation passed")
		return nil
	}
	if err := a.jira.AddComment(ctx, key, *body); err != nil {
		return err
	}
	if err := a.store.LogOperation(ctx, "comment", key, mustJSON(map[string]string{"body": *body}), "ok"); err != nil {
		return err
	}
	if err := a.syncIssueByKey(ctx, key); err != nil {
		return err
	}
	if *outputJSON {
		return printJSON(map[string]any{"commented": key})
	}
	fmt.Printf("comment added to %s\n", key)
	return nil
}

func (a *App) runTransition(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("transition", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	to := fs.String("to", "", "Target transition name or id")
	dryRun := fs.Bool("dry-run", false, "Validate only")
	outputJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &UsageError{Message: "usage: jirax transition ISSUE-123 --to 'Done' [--dry-run]"}
	}
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureStore(); err != nil {
		return err
	}
	if err := a.syncIfNeeded(ctx); err != nil {
		return err
	}
	key := fs.Arg(0)
	if *to == "" {
		return errors.New("--to is required")
	}
	if err := a.syncIssueByKey(ctx, key); err != nil {
		return err
	}

	transitions, err := a.jira.GetTransitions(ctx, key)
	if err != nil {
		return err
	}
	match, ok := FindTransition(transitions, *to)
	if !ok {
		return fmt.Errorf("transition %q not available", *to)
	}
	if *dryRun {
		payload := map[string]any{"valid": true, "issue": key, "transition": match}
		if *outputJSON {
			return printJSON(payload)
		}
		fmt.Printf("transition %s -> %s is valid\n", key, match.Name)
		return nil
	}
	if err := a.jira.TransitionIssue(ctx, key, match.ID); err != nil {
		return err
	}
	if err := a.store.LogOperation(ctx, "transition", key, mustJSON(match), "ok"); err != nil {
		return err
	}
	if err := a.syncIssueByKey(ctx, key); err != nil {
		return err
	}
	if *outputJSON {
		return printJSON(map[string]any{"transitioned": key, "to": match.Name})
	}
	fmt.Printf("transitioned %s to %s\n", key, match.Name)
	return nil
}

func (a *App) requireConfigured() error {
	if a.config == nil {
		return errors.New("config not loaded")
	}
	if err := a.config.ApplyEnvFallbacks(); err != nil {
		return err
	}
	if err := a.config.ValidateContext(); err != nil {
		return err
	}
	if !a.config.HasServer() {
		return errors.New("jira server config missing; set via `jirax ctx set --base-url --user --token ...` or env vars")
	}
	if a.jira == nil {
		a.jira = NewJiraClient(a.config)
	}
	return nil
}

func (a *App) syncIfNeeded(ctx context.Context) error {
	if err := a.ensureStore(); err != nil {
		return err
	}
	lastSync, err := a.store.LastSyncTime(ctx, a.config.Context.Name)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if a.now != nil {
		now = a.now().UTC()
	}

	decision := decideFreshnessAction(now, lastSync, a.config.Sync)
	switch decision {
	case freshnessActionSkip:
		return nil
	case freshnessActionSync:
		_, err := a.syncIssues(ctx, SyncOptions{})
		return err
	case freshnessActionCheck:
		hasUpdates, err := a.hasRemoteUpdatesSince(ctx, lastSync)
		if err != nil {
			if a.config.Sync.AllowStaleOnError && !lastSync.IsZero() && now.Sub(lastSync) < a.config.Sync.MaxStaleness() {
				return nil
			}
			return err
		}
		if !hasUpdates {
			return nil
		}
		_, err = a.syncIssues(ctx, SyncOptions{})
		return err
	default:
		return nil
	}
}

func (a *App) hasRemoteUpdatesSince(ctx context.Context, lastSync time.Time) (bool, error) {
	issues, err := a.jira.SearchIssues(ctx, SearchOptions{
		JQL:        a.config.Context.ScopeJQL(),
		Fields:     []string{"updated"},
		UpdatedAt:  lastSync,
		Full:       false,
		MaxResults: 1,
	})
	if err != nil {
		return false, err
	}
	return len(issues) > 0, nil
}

type freshnessAction string

const (
	freshnessActionSkip  freshnessAction = "skip"
	freshnessActionCheck freshnessAction = "check"
	freshnessActionSync  freshnessAction = "sync"
)

func decideFreshnessAction(now, lastSync time.Time, cfg SyncConfig) freshnessAction {
	if lastSync.IsZero() {
		return freshnessActionSync
	}
	age := now.Sub(lastSync)
	if age >= cfg.MaxStaleness() {
		return freshnessActionSync
	}
	if age >= cfg.CheckInterval() {
		return freshnessActionCheck
	}
	return freshnessActionSkip
}

func (a *App) syncIssueByKey(ctx context.Context, key string) error {
	if err := a.ensureStore(); err != nil {
		return err
	}
	issue, err := a.jira.GetIssue(ctx, key)
	if err != nil {
		return err
	}
	fields, err := a.jira.FieldCatalog(ctx)
	if err != nil {
		return err
	}
	if err := a.store.UpsertFieldCatalog(ctx, fields); err != nil {
		return err
	}
	return a.store.UpsertIssueBundle(ctx, issue, a.config.Context)
}

func usageText() string {
	return strings.TrimSpace(`
jirax — local-first Jira CLI

Commands:
  jirax config show [--json]
  jirax ctx set --project KEY | --projects A,B | --jql QUERY [--base-url URL --user USER --token TOKEN --ca-cert-file PATH --insecure-skip-verify --fields a,b --sync-check-minutes N --sync-max-stale-minutes N]
  jirax know [overview|fields|statuses|types|transitions ISSUE-123] [--json]
  jirax sync [--full] [--json]
  jirax issue ISSUE-123 [--json]
  jirax search [query] [--jql QUERY] [--mode text|local|remote|auto] [--limit N] [--json]
  jirax create --project KEY --type Task --summary "..." [--description "..."] [--fields-json '{}'] [--dry-run]
  jirax edit ISSUE-123 --fields-json '{"summary":"..."}' [--dry-run]
  jirax comment ISSUE-123 --body "..." [--dry-run]
  jirax transition ISSUE-123 --to "Done" [--dry-run]

Environment:
  JIRAX_BASE_URL
  JIRAX_USER
  JIRAX_TOKEN
  JIRAX_CA_CERT_FILE
  JIRAX_INSECURE_SKIP_VERIFY
  JIRAX_DB_PATH
`)
}

func (a *App) searchIssues(ctx context.Context, textQuery, jqlQuery, mode string, limit int) ([]IssueView, string, string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "auto"
	}

	if strings.TrimSpace(jqlQuery) == "" {
		if mode == "local" || mode == "remote" || mode == "auto" || mode == "text" {
			results, err := a.store.SearchIssues(ctx, textQuery, limit)
			return results, "text", "", err
		}
		return nil, "", "", fmt.Errorf("unsupported search mode %q", mode)
	}

	combinedJQL := combineJQL(a.config.Context.ScopeJQL(), jqlQuery)
	switch mode {
	case "text":
		return nil, "", "", errors.New("--jql cannot be used with --mode text")
	case "local":
		results, err := a.store.SearchIssuesByJQL(ctx, combinedJQL, limit)
		return results, "local", combinedJQL, err
	case "remote":
		results, err := a.searchIssuesRemote(ctx, combinedJQL, limit)
		return results, "remote", combinedJQL, err
	case "auto":
		results, err := a.store.SearchIssuesByJQL(ctx, combinedJQL, limit)
		if err == nil {
			return results, "local", combinedJQL, nil
		}
		results, remoteErr := a.searchIssuesRemote(ctx, combinedJQL, limit)
		if remoteErr != nil {
			return nil, "", combinedJQL, fmt.Errorf("local JQL unsupported (%v) and remote search failed (%v)", err, remoteErr)
		}
		return results, "remote", combinedJQL, nil
	default:
		return nil, "", "", fmt.Errorf("unsupported search mode %q", mode)
	}
}

func (a *App) searchIssuesRemote(ctx context.Context, jql string, limit int) ([]IssueView, error) {
	issues, err := a.jira.SearchIssues(ctx, SearchOptions{
		JQL:        jql,
		Fields:     a.config.Context.AllFields(),
		Full:       true,
		MaxResults: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]IssueView, 0, len(issues))
	for _, issue := range issues {
		view, _, _, _, normErr := normalizeIssue(issue)
		if normErr != nil {
			return nil, normErr
		}
		out = append(out, *view)
	}
	return out, nil
}

func combineJQL(scope, extra string) string {
	scope = strings.TrimSpace(scope)
	extra = strings.TrimSpace(extra)
	switch {
	case scope == "":
		return extra
	case extra == "":
		return scope
	default:
		return fmt.Sprintf("(%s) AND (%s)", scope, extra)
	}
}

type SyncOptions struct {
	Full bool
}

type SyncResult struct {
	Scanned    int       `json:"scanned"`
	Changed    int       `json:"changed"`
	FinishedAt time.Time `json:"finished_at"`
}

func (a *App) syncIssues(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	if err := a.ensureStore(); err != nil {
		return nil, err
	}
	lastSync, err := a.store.LastSyncTime(ctx, a.config.Context.Name)
	if err != nil {
		return nil, err
	}
	fieldCatalog, err := a.jira.FieldCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.store.UpsertFieldCatalog(ctx, fieldCatalog); err != nil {
		return nil, err
	}

	searchOpts := SearchOptions{
		JQL:       a.config.Context.ScopeJQL(),
		Fields:    a.config.Context.AllFields(),
		UpdatedAt: lastSync,
		Full:      opts.Full,
	}
	issues, err := a.jira.SearchIssues(ctx, searchOpts)
	if err != nil {
		return nil, err
	}

	changed := 0
	for _, issue := range issues {
		if err := a.store.UpsertIssueBundle(ctx, issue, a.config.Context); err != nil {
			return nil, err
		}
		changed++
	}
	now := time.Now().UTC()
	if a.now != nil {
		now = a.now().UTC()
	}
	if err := a.store.RecordSync(ctx, a.config.Context.Name, now); err != nil {
		return nil, err
	}

	return &SyncResult{
		Scanned:    len(issues),
		Changed:    changed,
		FinishedAt: now,
	}, nil
}

func defaultConfigPath() (string, error) {
	discovery, err := DiscoverConfig("")
	if err != nil {
		return "", err
	}
	localDir := filepath.Dir(discovery.OverlayConfig)
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return "", err
	}
	return discovery.OverlayConfig, nil
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseJSONObject(v string) (map[string]any, error) {
	if strings.TrimSpace(v) == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}
	return out, nil
}
