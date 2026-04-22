package jirax

import (
	"encoding/json"
	"fmt"
	"os"
)

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printIssue(issue *IssueView) {
	fmt.Printf("%s  %s\n", issue.Key, issue.Summary)
	fmt.Printf("status: %s\n", issue.Status)
	if issue.IssueType != "" {
		fmt.Printf("type: %s\n", issue.IssueType)
	}
	if issue.Assignee != "" {
		fmt.Printf("assignee: %s\n", issue.Assignee)
	}
	if issue.UpdatedAt != "" {
		fmt.Printf("updated: %s\n", issue.UpdatedAt)
	}
	if issue.Description != "" {
		fmt.Printf("\n%s\n", issue.Description)
	}
	if len(issue.Comments) > 0 {
		fmt.Printf("\ncomments: %d\n", len(issue.Comments))
	}
	if len(issue.Changelog) > 0 {
		fmt.Printf("changelog entries: %d\n", len(issue.Changelog))
	}
}
