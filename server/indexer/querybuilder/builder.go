package querybuilder

import (
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/nogon/files"
	"github.com/asciimoo/nogon/server/indexer/searchschema"
	"github.com/asciimoo/nogon/server/languages"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

var (
	relativeTimeFilterPattern = regexp.MustCompile(`^(<=|>=|<|>)([0-9]+)([smhdw])$`)
	absoluteDateFilterPattern = regexp.MustCompile(`^(<=|>=|<|>)([0-9]{4}-[0-9]{2}-[0-9]{2})$`)
	ErrInvalidRegexp          = errors.New("invalid URL regexp")
)

const MaxURLRegexpLength = 4096

// BuildValidated rejects malformed syntax and URL regular expressions before
// compiling the query. It never falls back to a different query language.
func BuildValidated(s string) (query.Query, error) {
	tokens, err := Tokenize(s)
	if err != nil {
		return nil, err
	}
	if err := validateURLRegexps(tokens); err != nil {
		return nil, err
	}
	root, err := parseExpression(tokens)
	if err != nil {
		return nil, err
	}
	return buildExpression(root), nil
}

func validateURLRegexps(tokens []Token) error {
	for i, token := range tokens {
		if token.Type != TokenField || token.Value != "url_re" {
			continue
		}
		pattern := ""
		location := token
		if i+1 < len(tokens) && (tokens[i+1].Type == TokenWord || tokens[i+1].Type == TokenQuoted) {
			pattern = tokens[i+1].Value
			location = tokens[i+1]
		}
		if pattern == "" {
			return diagnostic(location, "%w: expression is empty", ErrInvalidRegexp)
		}
		if len(pattern) > MaxURLRegexpLength {
			return diagnostic(location, "%w: expression exceeds %d bytes", ErrInvalidRegexp, MaxURLRegexpLength)
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return diagnostic(location, "%w: %v", ErrInvalidRegexp, err)
		}
	}
	return nil
}

// Build matches nothing on invalid input. Use BuildValidated to report errors.
func Build(s string) query.Query {
	q, err := BuildValidated(s)
	if err != nil {
		return query.NewMatchNoneQuery()
	}
	return q
}

// RemoveStandaloneWildcards returns text suitable for semantic embedding.
func RemoveStandaloneWildcards(s string) string {
	tokens, err := Tokenize(s)
	if err != nil {
		return s
	}
	root, err := parseExpression(tokens)
	if err != nil {
		return s
	}
	return simplifyExpression(root, "").embeddingText()
}

func createCombinedMatchQuery(s string, boost float64) query.Query {
	q := bleve.NewDisjunctionQuery(
		createMatchPhraseQuery(s, boost*10),
		createMatchQuery(s, boost),
	)
	q.SetBoost(boost)
	return q
}

// Matches all terms from the query (AND semantics per field)
func createMatchQuery(s string, boost float64) query.Query {
	queries := make([]query.Query, 0)
	for _, field := range searchschema.Fields() {
		if !field.DefaultSearch {
			continue
		}
		match := bleve.NewMatchQuery(s)
		match.SetField(field.IndexField)
		match.SetBoost(field.Weight)
		match.Operator = query.MatchQueryOperatorAnd
		queries = append(queries, match)
	}
	q := bleve.NewDisjunctionQuery(queries...)
	q.SetBoost(boost)
	return q
}

// buildFieldQuery creates the appropriate Bleve query for a named field and value.
// It uses a case insensitive RegexpQuery for URL wildcards, WildcardQuery for
// other wildcard fields, TermQuery for URL and domain fields, and MatchQuery
// for all other fields.
func buildFieldQuery(field, v string) query.Query {
	definition, ok := searchschema.Field(field)
	if !ok {
		return bleve.NewQueryStringQuery(v)
	}
	if definition.Name == "language" {
		return buildLanguageQuery(definition, v)
	}
	if strings.Contains(v, "*") {
		if definition.NormalizeFilePath {
			q := bleve.NewRegexpQuery(caseInsensitiveWildcardRegexp(v))
			q.SetField(definition.IndexField)
			q.SetBoost(definition.Weight)
			return q
		}
		q := bleve.NewWildcardQuery(strings.ToLower(v))
		q.SetField(definition.IndexField)
		q.SetBoost(definition.Weight)
		return q
	}
	if definition.Kind == searchschema.FieldKindKeyword {
		if definition.NormalizeFilePath {
			v = normalizeFileURL(v)
		}
		q := bleve.NewTermQuery(v)
		q.SetField(definition.IndexField)
		q.SetBoost(definition.Weight)
		return q
	}
	q := bleve.NewMatchQuery(v)
	q.SetField(definition.IndexField)
	q.SetBoost(definition.Weight)
	return q
}

func caseInsensitiveWildcardRegexp(value string) string {
	var pattern strings.Builder
	pattern.WriteString("(?i)")
	for _, r := range value {
		switch r {
		case '*':
			pattern.WriteString(".*")
		case '?':
			pattern.WriteByte('.')
		default:
			pattern.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return pattern.String()
}

// Language filters use normalized tags, include more specific tags, and keep
// historical aliases searchable without requiring an index rebuild.
func buildLanguageQuery(field searchschema.FieldDefinition, value string) query.Query {
	var tags []string
	wildcard := strings.ContainsAny(value, "*?")
	if wildcard {
		// Normalize the base alias when it is specified, preserving the pattern.
		base, suffix, hasSuffix := strings.Cut(strings.ToLower(value), "-")
		if resolved, err := languages.Resolve(base); err == nil {
			tags = languages.StoredTags(resolved.Tag)
			if hasSuffix {
				for i := range tags {
					tags[i] += "-" + suffix
				}
			}
		} else {
			tags = []string{value}
		}
	} else {
		resolved, err := languages.Resolve(value)
		if err != nil {
			return query.NewMatchNoneQuery()
		}
		tags = languages.StoredTags(resolved.Tag)
	}
	queries := make([]query.Query, 0, len(tags))
	for _, tag := range tags {
		pattern := "(?i)" + regexp.QuoteMeta(tag) + "(-.*)?"
		if wildcard {
			pattern = caseInsensitiveWildcardRegexp(tag)
		}
		q := bleve.NewRegexpQuery(pattern)
		q.SetField(field.IndexField)
		q.SetBoost(field.Weight)
		queries = append(queries, q)
	}
	return bleve.NewDisjunctionQuery(queries...)
}

// buildQuotedFieldQuery preserves phrase semantics for analyzed text fields.
// URL and other fields retain their existing query behavior.
func buildQuotedFieldQuery(field, v string) query.Query {
	definition, ok := searchschema.Field(field)
	if ok && definition.Kind == searchschema.FieldKindRegexp {
		return buildURLRegexpQuery(definition, v)
	}
	if ok && definition.Phrase {
		q := bleve.NewMatchPhraseQuery(v)
		q.SetField(definition.IndexField)
		q.SetBoost(definition.Weight)
		return q
	}
	return buildFieldQuery(field, v)
}

func buildNumericRangeQuery(field searchschema.FieldDefinition, v string) (query.Query, bool) {
	parseBound := func(s string) (*float64, bool) {
		if s == "" {
			return nil, true
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, false
		}
		return &n, true
	}

	var min, max *float64
	if before, after, ok := strings.Cut(v, ".."); ok {
		var valid bool
		min, valid = parseBound(before)
		if !valid {
			return nil, false
		}
		max, valid = parseBound(after)
		if !valid {
			return nil, false
		}
	} else {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, false
		}
		min = &n
		max = &n
	}
	if min == nil && max == nil {
		return nil, false
	}
	q := bleve.NewNumericRangeInclusiveQuery(min, max, new(true), new(true))
	q.SetField(field.IndexField)
	return q, true
}

func parseRelativeTimeFilter(v string) (string, int64, bool) {
	parts := relativeTimeFilterPattern.FindStringSubmatch(strings.ToLower(v))
	if parts == nil {
		return "", 0, false
	}
	amount, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", 0, false
	}
	var unitSeconds int64
	switch parts[3] {
	case "s":
		unitSeconds = 1
	case "m":
		unitSeconds = 60
	case "h":
		unitSeconds = 60 * 60
	case "d":
		unitSeconds = 24 * 60 * 60
	case "w":
		unitSeconds = 7 * 24 * 60 * 60
	default:
		return "", 0, false
	}
	if amount > (1<<63-1)/unitSeconds {
		return "", 0, false
	}
	return parts[1], amount * unitSeconds, true
}

func parseAbsoluteDateFilter(v string) (string, int64, bool) {
	parts := absoluteDateFilterPattern.FindStringSubmatch(v)
	if parts == nil {
		return "", 0, false
	}
	date, err := time.Parse("2006-01-02", parts[2])
	if err != nil {
		return "", 0, false
	}
	return parts[1], date.Unix(), true
}

// BuildTimestampRange creates a numeric timestamp range for an indexed time
// field. It is shared by query syntax and legacy structured API parameters.
func BuildTimestampRange(field string, min, max *int64, minInclusive, maxInclusive bool) (query.Query, bool) {
	definition, ok := searchschema.Field(field)
	if !ok || definition.Kind != searchschema.FieldKindTime || (min == nil && max == nil) {
		return nil, false
	}
	var minValue, maxValue *float64
	if min != nil {
		minValue = new(float64)
		*minValue = float64(*min)
	}
	if max != nil {
		maxValue = new(float64)
		*maxValue = float64(*max)
	}
	q := bleve.NewNumericRangeInclusiveQuery(minValue, maxValue, &minInclusive, &maxInclusive)
	q.SetField(definition.IndexField)
	return q, true
}

func buildTimeQuery(fieldName, v string, now time.Time) (query.Query, bool) {
	field, ok := searchschema.Field(fieldName)
	if !ok || field.Kind != searchschema.FieldKindTime {
		return nil, false
	}
	comparison, durationSeconds, relative := parseRelativeTimeFilter(v)
	var cutoff int64
	if relative {
		cutoff = now.Unix() - durationSeconds
		switch comparison {
		case ">":
			comparison = "<"
		case ">=":
			comparison = "<="
		case "<":
			comparison = ">"
		case "<=":
			comparison = ">="
		}
	} else {
		var timestamp int64
		var ok bool
		comparison, timestamp, ok = parseAbsoluteDateFilter(v)
		if !ok {
			return nil, false
		}
		cutoff = timestamp
	}
	var min, max *int64
	minInclusive := true
	maxInclusive := true
	switch comparison {
	case ">":
		min = &cutoff
		minInclusive = false
	case ">=":
		min = &cutoff
	case "<":
		max = &cutoff
		maxInclusive = false
	case "<=":
		max = &cutoff
	default:
		return nil, false
	}
	return BuildTimestampRange(field.Name, min, max, minInclusive, maxInclusive)
}

// Matches exact phrases without stopwords
func createMatchPhraseQuery(s string, boost float64) query.Query {
	queries := make([]query.Query, 0)
	for _, field := range searchschema.Fields() {
		if !field.DefaultSearch {
			continue
		}
		match := bleve.NewMatchPhraseQuery(s)
		match.SetField(field.IndexField)
		match.SetBoost(field.Weight)
		queries = append(queries, match)
	}
	q := bleve.NewDisjunctionQuery(queries...)
	q.SetBoost(boost)
	return q
}

func buildValueQuery(fieldName, value string, phrase bool, now time.Time) query.Query {
	if strings.HasPrefix(fieldName, "metadata.") {
		q := bleve.NewTermQuery(value)
		q.SetField(fieldName)
		return q
	}
	if field, ok := searchschema.Field(fieldName); ok {
		switch field.Kind {
		case searchschema.FieldKindSite:
			return buildSiteQuery(field, value)
		case searchschema.FieldKindExists:
			return buildPresenceQuery(value)
		case searchschema.FieldKindRegexp:
			return buildURLRegexpQuery(field, value)
		case searchschema.FieldKindEnum:
			if v, ok := searchschema.Value(field.ValueSet, value); ok {
				q := bleve.NewNumericRangeQuery(v.Min, v.Max)
				q.SetField(field.IndexField)
				return q
			}
		case searchschema.FieldKindNumericRange:
			if q, ok := buildNumericRangeQuery(field, value); ok {
				return q
			}
		case searchschema.FieldKindTime:
			if q, ok := buildTimeQuery(field.Name, value, now); ok {
				return q
			}
		case searchschema.FieldKindInteger:
			if number, err := strconv.ParseUint(value, 10, 64); err == nil {
				v := float64(number)
				q := bleve.NewNumericRangeInclusiveQuery(&v, &v, new(true), new(true))
				q.SetField(field.IndexField)
				return q
			}
		case searchschema.FieldKindText, searchschema.FieldKindKeyword:
			if phrase {
				return buildQuotedFieldQuery(field.Name, value)
			}
			return buildFieldQuery(field.Name, value)
		}
		// Unknown values keep the existing fallback to ordinary search text.
		value = fieldName + ":" + value
	}
	if phrase {
		return createMatchPhraseQuery(value, 1)
	}
	wildcard := value
	if !strings.HasPrefix(wildcard, "*") {
		wildcard = "*" + wildcard
	}
	if !strings.HasSuffix(wildcard, "*") {
		wildcard += "*"
	}
	var queries []query.Query
	for _, field := range searchschema.Fields() {
		switch {
		case field.DefaultSearch:
			queries = append(queries, buildFieldQuery(field.Name, value))
		case field.DefaultWildcard:
			queries = append(queries, buildFieldQuery(field.Name, wildcard))
		}
	}
	return bleve.NewDisjunctionQuery(queries...)
}

func normalizeFileURL(v string) string {
	if strings.HasPrefix(v, "*") {
		return v
	}
	if strings.Contains(v, "://") {
		return v
	}
	if abs, err := filepath.Abs(v); err == nil {
		v = abs
	}
	return files.PathToFileURL(v)
}
