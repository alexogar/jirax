# Jirax — Local-first Jira CLI for Humans and Agents

## 1. Overview

Jirax is a local-first CLI tool that syncs a scoped subset of Jira data into a local SQLite database to enable:

- sub-second queries
- agent-friendly outputs
- offline-ish workflows
- schema normalization over Jira custom field chaos
- safe, validated mutations back to Jira

Unlike traditional Jira CLIs, Jirax separates:
- reads → local database
- writes → controlled API mutations

## 2. Goals

### Primary goals
- Fast local queries (no API latency)
- Sync on every command (incremental + scoped)
- Support Jira Cloud + Data Center + private URLs
- Handle arbitrary custom fields
- Provide agent-friendly CLI + JSON interface
- Safe mutation workflow

### Non-goals (V1)
- Full bidirectional sync of everything
- Perfect schema normalization across all Jira instances
- UI/TUI
- Plugin system

## 3. Core Concepts

### Context (Scope)
Every command operates inside a context.

Context defines:
- which issues are synced
- which fields are indexed
- which metadata is cached

Types:
- project-based
- JQL filter-based

## 4. Sync Model

- Sync runs automatically on every command
- Incremental sync based on updated timestamp
- Targeted sync before writes
- Bootstrap sync for new context

## 5. Storage (SQLite)

Core tables:
- issues
- issue_raw
- comments
- changelog
- field_catalog
- field_aliases
- operation_log

## 6. Search & Indexing

- SQLite FTS5 for full-text search
- Indexed core fields
- Extracted custom field values

## 7. Field System

Supports:
- automatic field discovery
- alias mapping
- extraction rules

## 8. Commands

- jirax ctx set
- jirax sync
- jirax issue
- jirax search
- jirax create
- jirax edit
- jirax comment
- jirax transition

## 9. Issue Creation

- Uses Jira metadata endpoints
- Validates required fields
- Supports JSON input

## 10. Mutation Safety

- refresh before write
- dry-run support
- validation

## 11. Server Support

- Jira Cloud
- Jira Data Center
- custom/private URLs

## 12. Output Format

JSON output supported for all commands

## 13. Tech Stack

- Go
- SQLite
- REST API

## 14. Principles

- local-first
- sync-on-command
- scoped data
- schema flexibility
- agent-first UX
- safe writes
