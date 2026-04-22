package jirax

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var errUnsupportedLocalJQL = errors.New("unsupported local JQL")

type localJQLQuery struct {
	filter  localJQLExpr
	orderBy []localJQLSort
}

type localJQLSort struct {
	field string
	desc  bool
}

type localJQLExpr interface {
	Eval(issue *IssueView, aliases map[string]string) (bool, error)
}

type localJQLBinaryExpr struct {
	op    string
	left  localJQLExpr
	right localJQLExpr
}

type localJQLNotExpr struct {
	expr localJQLExpr
}

type localJQLComparison struct {
	field    string
	operator string
	values   []string
}

func parseLocalJQL(input string) (*localJQLQuery, error) {
	tokens, err := tokenizeLocalJQL(input)
	if err != nil {
		return nil, err
	}
	parser := &localJQLParser{tokens: tokens}
	query, err := parser.parseQuery()
	if err != nil {
		return nil, err
	}
	if !parser.isAtEnd() {
		return nil, fmt.Errorf("%w: unexpected token %q", errUnsupportedLocalJQL, parser.peek().literal)
	}
	return query, nil
}

func (e localJQLBinaryExpr) Eval(issue *IssueView, aliases map[string]string) (bool, error) {
	left, err := e.left.Eval(issue, aliases)
	if err != nil {
		return false, err
	}
	switch e.op {
	case "AND":
		if !left {
			return false, nil
		}
		return e.right.Eval(issue, aliases)
	case "OR":
		if left {
			return true, nil
		}
		return e.right.Eval(issue, aliases)
	default:
		return false, fmt.Errorf("%w: unsupported boolean operator %q", errUnsupportedLocalJQL, e.op)
	}
}

func (e localJQLNotExpr) Eval(issue *IssueView, aliases map[string]string) (bool, error) {
	result, err := e.expr.Eval(issue, aliases)
	if err != nil {
		return false, err
	}
	return !result, nil
}

func (e localJQLComparison) Eval(issue *IssueView, aliases map[string]string) (bool, error) {
	field := resolveLocalJQLField(e.field, aliases)
	values, ok := localJQLFieldValues(issue, field)
	if !ok {
		return false, fmt.Errorf("%w: field %q is not available in the local cache", errUnsupportedLocalJQL, e.field)
	}

	switch e.operator {
	case "IS EMPTY":
		return len(values) == 0 || allEmpty(values), nil
	case "IS NOT EMPTY":
		return len(values) > 0 && !allEmpty(values), nil
	}

	if len(e.values) == 0 {
		return false, fmt.Errorf("%w: missing comparison value for %q", errUnsupportedLocalJQL, e.field)
	}

	switch e.operator {
	case "=", "!=", "~", "!~":
		return evalLocalJQLMatch(values, e.operator, e.values[0]), nil
	case "IN", "NOT IN":
		match := false
		for _, want := range e.values {
			if evalLocalJQLMatch(values, "=", want) {
				match = true
				break
			}
		}
		if e.operator == "NOT IN" {
			return !match, nil
		}
		return match, nil
	case ">", ">=", "<", "<=":
		return evalLocalJQLCompare(field, values, e.operator, e.values[0])
	default:
		return false, fmt.Errorf("%w: operator %q is not supported locally", errUnsupportedLocalJQL, e.operator)
	}
}

func evalLocalJQLMatch(values []string, operator, want string) bool {
	wantFold := strings.ToLower(strings.TrimSpace(want))
	if len(values) == 0 {
		return operator == "!=" || operator == "!~"
	}
	if operator == "!=" || operator == "!~" {
		for _, value := range values {
			gotFold := strings.ToLower(strings.TrimSpace(value))
			switch operator {
			case "!=":
				if gotFold == wantFold {
					return false
				}
			case "!~":
				if strings.Contains(gotFold, wantFold) {
					return false
				}
			}
		}
		return true
	}
	for _, value := range values {
		got := strings.TrimSpace(value)
		gotFold := strings.ToLower(got)
		switch operator {
		case "=":
			if gotFold == wantFold {
				return true
			}
		case "~":
			if strings.Contains(gotFold, wantFold) {
				return true
			}
		}
	}
	return false
}

func evalLocalJQLCompare(field string, values []string, operator, want string) (bool, error) {
	for _, value := range values {
		ok, err := compareLocalJQLValue(field, value, operator, want)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func compareLocalJQLValue(field, got, operator, want string) (bool, error) {
	if isLocalJQLTimeField(field) {
		gotTime, err := parseLocalJQLTime(got)
		if err != nil {
			return false, fmt.Errorf("%w: could not parse %q as time", errUnsupportedLocalJQL, got)
		}
		wantTime, err := parseLocalJQLTime(want)
		if err != nil {
			return false, fmt.Errorf("%w: could not parse %q as time", errUnsupportedLocalJQL, want)
		}
		switch operator {
		case ">":
			return gotTime.After(wantTime), nil
		case ">=":
			return gotTime.After(wantTime) || gotTime.Equal(wantTime), nil
		case "<":
			return gotTime.Before(wantTime), nil
		case "<=":
			return gotTime.Before(wantTime) || gotTime.Equal(wantTime), nil
		}
	}

	gotNum, gotErr := strconv.ParseFloat(strings.TrimSpace(got), 64)
	wantNum, wantErr := strconv.ParseFloat(strings.TrimSpace(want), 64)
	if gotErr == nil && wantErr == nil {
		switch operator {
		case ">":
			return gotNum > wantNum, nil
		case ">=":
			return gotNum >= wantNum, nil
		case "<":
			return gotNum < wantNum, nil
		case "<=":
			return gotNum <= wantNum, nil
		}
	}

	gotFold := strings.ToLower(strings.TrimSpace(got))
	wantFold := strings.ToLower(strings.TrimSpace(want))
	switch operator {
	case ">":
		return gotFold > wantFold, nil
	case ">=":
		return gotFold >= wantFold, nil
	case "<":
		return gotFold < wantFold, nil
	case "<=":
		return gotFold <= wantFold, nil
	default:
		return false, fmt.Errorf("%w: operator %q is not supported locally", errUnsupportedLocalJQL, operator)
	}
}

func parseLocalJQLTime(value string) (time.Time, error) {
	value = strings.TrimSpace(strings.Trim(value, `"'`))
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006-01-02T15:04:05.000-0700",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %s", value)
}

func localJQLFieldValues(issue *IssueView, field string) ([]string, bool) {
	switch field {
	case "issue", "key", "issuekey":
		return localJQLStrings(issue.Key), true
	case "project", "projectkey":
		return localJQLStrings(issue.Project), true
	case "summary":
		return localJQLStrings(issue.Summary), true
	case "description":
		return localJQLStrings(issue.Description), true
	case "status":
		return localJQLStrings(issue.Status), true
	case "type", "issuetype", "issue_type":
		return localJQLStrings(issue.IssueType), true
	case "priority":
		return localJQLStrings(issue.Priority), true
	case "assignee":
		return localJQLStrings(issue.Assignee), true
	case "reporter":
		return localJQLStrings(issue.Reporter), true
	case "created", "created_at":
		return localJQLStrings(issue.CreatedAt), true
	case "updated", "updated_at":
		return localJQLStrings(issue.UpdatedAt), true
	case "labels", "label":
		if len(issue.Labels) == 0 {
			return nil, true
		}
		return append([]string(nil), issue.Labels...), true
	default:
		if issue.CustomFields == nil {
			return nil, false
		}
		value, ok := issue.CustomFields[field]
		if !ok {
			return nil, false
		}
		return localJQLAnyValues(value), true
	}
}

func localJQLAnyValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, flattenText(item))
		}
		return compactLocalJQLStrings(out)
	default:
		return localJQLStrings(flattenText(value))
	}
}

func localJQLStrings(values ...string) []string {
	return compactLocalJQLStrings(values)
}

func compactLocalJQLStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolveLocalJQLField(field string, aliases map[string]string) string {
	field = normalizeLocalJQLIdentifier(field)
	if resolved, ok := aliases[field]; ok {
		return normalizeLocalJQLIdentifier(resolved)
	}
	return field
}

func normalizeLocalJQLIdentifier(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
}

func allEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func isLocalJQLTimeField(field string) bool {
	switch normalizeLocalJQLIdentifier(field) {
	case "created", "created_at", "updated", "updated_at":
		return true
	default:
		return false
	}
}

func sortIssuesByLocalJQL(issues []IssueView, sorts []localJQLSort, aliases map[string]string) {
	if len(sorts) == 0 {
		slices.SortStableFunc(issues, func(a, b IssueView) int {
			return compareLocalJQLSortField("updated", true, &a, &b, aliases)
		})
		return
	}

	slices.SortStableFunc(issues, func(a, b IssueView) int {
		for _, sort := range sorts {
			if cmp := compareLocalJQLSortField(sort.field, sort.desc, &a, &b, aliases); cmp != 0 {
				return cmp
			}
		}
		return strings.Compare(a.Key, b.Key)
	})
}

func compareLocalJQLSortField(field string, desc bool, a, b *IssueView, aliases map[string]string) int {
	field = resolveLocalJQLField(field, aliases)
	aValues, _ := localJQLFieldValues(a, field)
	bValues, _ := localJQLFieldValues(b, field)
	aValue := ""
	bValue := ""
	if len(aValues) > 0 {
		aValue = aValues[0]
	}
	if len(bValues) > 0 {
		bValue = bValues[0]
	}

	cmp := 0
	if isLocalJQLTimeField(field) {
		aTime, aErr := parseLocalJQLTime(aValue)
		bTime, bErr := parseLocalJQLTime(bValue)
		switch {
		case aErr == nil && bErr == nil:
			if aTime.Before(bTime) {
				cmp = -1
			} else if aTime.After(bTime) {
				cmp = 1
			}
		default:
			cmp = strings.Compare(strings.ToLower(aValue), strings.ToLower(bValue))
		}
	} else {
		cmp = strings.Compare(strings.ToLower(aValue), strings.ToLower(bValue))
	}
	if desc {
		return -cmp
	}
	return cmp
}

type localJQLParser struct {
	tokens []localJQLToken
	pos    int
}

func (p *localJQLParser) parseQuery() (*localJQLQuery, error) {
	filter, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	var orderBy []localJQLSort
	if p.matchKeyword("ORDER") {
		if !p.matchKeyword("BY") {
			return nil, fmt.Errorf("%w: expected BY after ORDER", errUnsupportedLocalJQL)
		}
		orderBy, err = p.parseOrderBy()
		if err != nil {
			return nil, err
		}
	}
	return &localJQLQuery{filter: filter, orderBy: orderBy}, nil
}

func (p *localJQLParser) parseExpr() (localJQLExpr, error) {
	return p.parseOr()
}

func (p *localJQLParser) parseOr() (localJQLExpr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.matchKeyword("OR") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = localJQLBinaryExpr{op: "OR", left: left, right: right}
	}
	return left, nil
}

func (p *localJQLParser) parseAnd() (localJQLExpr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.matchKeyword("AND") {
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = localJQLBinaryExpr{op: "AND", left: left, right: right}
	}
	return left, nil
}

func (p *localJQLParser) parseUnary() (localJQLExpr, error) {
	if p.matchKeyword("NOT") {
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return localJQLNotExpr{expr: expr}, nil
	}
	return p.parsePrimary()
}

func (p *localJQLParser) parsePrimary() (localJQLExpr, error) {
	if p.match(localJQLTokenLParen) {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.match(localJQLTokenRParen) {
			return nil, fmt.Errorf("%w: expected )", errUnsupportedLocalJQL)
		}
		return expr, nil
	}
	return p.parseComparison()
}

func (p *localJQLParser) parseComparison() (localJQLExpr, error) {
	fieldToken, err := p.expect(localJQLTokenIdentifier, "expected field name")
	if err != nil {
		return nil, err
	}
	field := fieldToken.literal

	if p.matchKeyword("IS") {
		if p.matchKeyword("NOT") {
			if !p.matchKeyword("EMPTY") {
				return nil, fmt.Errorf("%w: expected EMPTY after IS NOT", errUnsupportedLocalJQL)
			}
			return localJQLComparison{field: field, operator: "IS NOT EMPTY"}, nil
		}
		if !p.matchKeyword("EMPTY") {
			return nil, fmt.Errorf("%w: expected EMPTY after IS", errUnsupportedLocalJQL)
		}
		return localJQLComparison{field: field, operator: "IS EMPTY"}, nil
	}

	if p.matchKeyword("NOT") {
		if !p.matchKeyword("IN") {
			return nil, fmt.Errorf("%w: expected IN after NOT", errUnsupportedLocalJQL)
		}
		values, err := p.parseValueList()
		if err != nil {
			return nil, err
		}
		return localJQLComparison{field: field, operator: "NOT IN", values: values}, nil
	}

	if p.matchKeyword("IN") {
		values, err := p.parseValueList()
		if err != nil {
			return nil, err
		}
		return localJQLComparison{field: field, operator: "IN", values: values}, nil
	}

	operatorToken := p.advance()
	switch operatorToken.typ {
	case localJQLTokenEqual, localJQLTokenNotEqual, localJQLTokenContains, localJQLTokenNotContains, localJQLTokenGreater, localJQLTokenGreaterEqual, localJQLTokenLess, localJQLTokenLessEqual:
	default:
		return nil, fmt.Errorf("%w: expected operator after field %q", errUnsupportedLocalJQL, field)
	}

	value, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	return localJQLComparison{field: field, operator: operatorToken.literal, values: []string{value}}, nil
}

func (p *localJQLParser) parseValueList() ([]string, error) {
	if !p.match(localJQLTokenLParen) {
		return nil, fmt.Errorf("%w: expected ( after IN", errUnsupportedLocalJQL)
	}
	var values []string
	for {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		if p.match(localJQLTokenComma) {
			continue
		}
		break
	}
	if !p.match(localJQLTokenRParen) {
		return nil, fmt.Errorf("%w: expected ) after value list", errUnsupportedLocalJQL)
	}
	return values, nil
}

func (p *localJQLParser) parseOrderBy() ([]localJQLSort, error) {
	var sorts []localJQLSort
	for {
		field, err := p.expect(localJQLTokenIdentifier, "expected ORDER BY field")
		if err != nil {
			return nil, err
		}
		sort := localJQLSort{field: field.literal}
		if p.matchKeyword("ASC") {
			sort.desc = false
		} else if p.matchKeyword("DESC") {
			sort.desc = true
		}
		sorts = append(sorts, sort)
		if !p.match(localJQLTokenComma) {
			break
		}
	}
	return sorts, nil
}

func (p *localJQLParser) parseValue() (string, error) {
	token := p.advance()
	switch token.typ {
	case localJQLTokenIdentifier, localJQLTokenString, localJQLTokenNumber:
		return token.literal, nil
	default:
		return "", fmt.Errorf("%w: expected value, got %q", errUnsupportedLocalJQL, token.literal)
	}
}

func (p *localJQLParser) expect(typ localJQLTokenType, message string) (localJQLToken, error) {
	token := p.advance()
	if token.typ != typ {
		return localJQLToken{}, fmt.Errorf("%w: %s", errUnsupportedLocalJQL, message)
	}
	return token, nil
}

func (p *localJQLParser) match(typ localJQLTokenType) bool {
	if p.peek().typ != typ {
		return false
	}
	p.pos++
	return true
}

func (p *localJQLParser) matchKeyword(keyword string) bool {
	token := p.peek()
	if token.typ != localJQLTokenIdentifier || !strings.EqualFold(token.literal, keyword) {
		return false
	}
	p.pos++
	return true
}

func (p *localJQLParser) isAtEnd() bool {
	return p.peek().typ == localJQLTokenEOF
}

func (p *localJQLParser) peek() localJQLToken {
	if p.pos >= len(p.tokens) {
		return localJQLToken{typ: localJQLTokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *localJQLParser) advance() localJQLToken {
	token := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return token
}

type localJQLTokenType int

const (
	localJQLTokenEOF localJQLTokenType = iota
	localJQLTokenIdentifier
	localJQLTokenString
	localJQLTokenNumber
	localJQLTokenLParen
	localJQLTokenRParen
	localJQLTokenComma
	localJQLTokenEqual
	localJQLTokenNotEqual
	localJQLTokenContains
	localJQLTokenNotContains
	localJQLTokenGreater
	localJQLTokenGreaterEqual
	localJQLTokenLess
	localJQLTokenLessEqual
)

type localJQLToken struct {
	typ     localJQLTokenType
	literal string
}

func tokenizeLocalJQL(input string) ([]localJQLToken, error) {
	var tokens []localJQLToken
	for i := 0; i < len(input); {
		ch := rune(input[i])
		switch {
		case unicode.IsSpace(ch):
			i++
		case ch == '(':
			tokens = append(tokens, localJQLToken{typ: localJQLTokenLParen, literal: "("})
			i++
		case ch == ')':
			tokens = append(tokens, localJQLToken{typ: localJQLTokenRParen, literal: ")"})
			i++
		case ch == ',':
			tokens = append(tokens, localJQLToken{typ: localJQLTokenComma, literal: ","})
			i++
		case ch == '"', ch == '\'':
			quote := ch
			i++
			start := i
			for i < len(input) && rune(input[i]) != quote {
				if input[i] == '\\' && i+1 < len(input) {
					i += 2
					continue
				}
				i++
			}
			if i >= len(input) {
				return nil, fmt.Errorf("%w: unterminated string", errUnsupportedLocalJQL)
			}
			tokens = append(tokens, localJQLToken{typ: localJQLTokenString, literal: input[start:i]})
			i++
		case strings.HasPrefix(input[i:], "!="):
			tokens = append(tokens, localJQLToken{typ: localJQLTokenNotEqual, literal: "!="})
			i += 2
		case strings.HasPrefix(input[i:], "!~"):
			tokens = append(tokens, localJQLToken{typ: localJQLTokenNotContains, literal: "!~"})
			i += 2
		case strings.HasPrefix(input[i:], ">="):
			tokens = append(tokens, localJQLToken{typ: localJQLTokenGreaterEqual, literal: ">="})
			i += 2
		case strings.HasPrefix(input[i:], "<="):
			tokens = append(tokens, localJQLToken{typ: localJQLTokenLessEqual, literal: "<="})
			i += 2
		case ch == '=':
			tokens = append(tokens, localJQLToken{typ: localJQLTokenEqual, literal: "="})
			i++
		case ch == '~':
			tokens = append(tokens, localJQLToken{typ: localJQLTokenContains, literal: "~"})
			i++
		case ch == '>':
			tokens = append(tokens, localJQLToken{typ: localJQLTokenGreater, literal: ">"})
			i++
		case ch == '<':
			tokens = append(tokens, localJQLToken{typ: localJQLTokenLess, literal: "<"})
			i++
		case unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-' || ch == '.':
			start := i
			for i < len(input) {
				r := rune(input[i])
				if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == ':') {
					break
				}
				i++
			}
			literal := input[start:i]
			typ := localJQLTokenIdentifier
			if _, err := strconv.ParseFloat(literal, 64); err == nil {
				typ = localJQLTokenNumber
			}
			tokens = append(tokens, localJQLToken{typ: typ, literal: literal})
		default:
			return nil, fmt.Errorf("%w: unexpected character %q", errUnsupportedLocalJQL, string(ch))
		}
	}
	tokens = append(tokens, localJQLToken{typ: localJQLTokenEOF})
	return tokens, nil
}
