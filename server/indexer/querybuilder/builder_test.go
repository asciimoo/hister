package querybuilder

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2/search/query"
)

// --- helpers ---

// buildBoolQ calls Build(s) and asserts the result is a *query.BooleanQuery.
func buildBoolQ(t *testing.T, s string) *query.BooleanQuery {
	t.Helper()
	q := Build(s)
	bq, ok := q.(*query.BooleanQuery)
	if !ok {
		t.Fatalf("Build(%q): expected *query.BooleanQuery, got %T", s, q)
	}
	return bq
}

// mustClauses extracts the Conjuncts from bq.Must, failing if Must is nil or the wrong type.
func mustClauses(t *testing.T, bq *query.BooleanQuery) []query.Query {
	t.Helper()
	if bq.Must == nil {
		t.Fatalf("BooleanQuery.Must is nil")
	}
	cq, ok := bq.Must.(*query.ConjunctionQuery)
	if !ok {
		t.Fatalf("BooleanQuery.Must: expected *query.ConjunctionQuery, got %T", bq.Must)
	}
	return cq.Conjuncts
}

// mustNotClauses extracts the Disjuncts from bq.MustNot, failing if MustNot is nil or the wrong type.
func mustNotClauses(t *testing.T, bq *query.BooleanQuery) []query.Query {
	t.Helper()
	if bq.MustNot == nil {
		t.Fatalf("BooleanQuery.MustNot is nil")
	}
	dq, ok := bq.MustNot.(*query.DisjunctionQuery)
	if !ok {
		t.Fatalf("BooleanQuery.MustNot: expected *query.DisjunctionQuery, got %T", bq.MustNot)
	}
	return dq.Disjuncts
}

// asDisjunction type-asserts q to *query.DisjunctionQuery.
func asDisjunction(t *testing.T, q query.Query) *query.DisjunctionQuery {
	t.Helper()
	dq, ok := q.(*query.DisjunctionQuery)
	if !ok {
		t.Fatalf("expected *query.DisjunctionQuery, got %T", q)
	}
	return dq
}

// asMatch type-asserts q to *query.MatchQuery.
func asMatch(t *testing.T, q query.Query) *query.MatchQuery {
	t.Helper()
	mq, ok := q.(*query.MatchQuery)
	if !ok {
		t.Fatalf("expected *query.MatchQuery, got %T", q)
	}
	return mq
}

// asTerm type-asserts q to *query.TermQuery.
func asTerm(t *testing.T, q query.Query) *query.TermQuery {
	t.Helper()
	tq, ok := q.(*query.TermQuery)
	if !ok {
		t.Fatalf("expected *query.TermQuery, got %T", q)
	}
	return tq
}

// asWildcard type-asserts q to *query.WildcardQuery.
func asWildcard(t *testing.T, q query.Query) *query.WildcardQuery {
	t.Helper()
	wq, ok := q.(*query.WildcardQuery)
	if !ok {
		t.Fatalf("expected *query.WildcardQuery, got %T", q)
	}
	return wq
}

// asRegexp type asserts q to *query.RegexpQuery.
func asRegexp(t *testing.T, q query.Query) *query.RegexpQuery {
	t.Helper()
	rq, ok := q.(*query.RegexpQuery)
	if !ok {
		t.Fatalf("expected *query.RegexpQuery, got %T", q)
	}
	return rq
}

// asMatchPhrase type-asserts q to *query.MatchPhraseQuery.
func asMatchPhrase(t *testing.T, q query.Query) *query.MatchPhraseQuery {
	t.Helper()
	mpq, ok := q.(*query.MatchPhraseQuery)
	if !ok {
		t.Fatalf("expected *query.MatchPhraseQuery, got %T", q)
	}
	return mpq
}

// asNumericRange type-asserts q to *query.NumericRangeQuery.
func asNumericRange(t *testing.T, q query.Query) *query.NumericRangeQuery {
	t.Helper()
	nq, ok := q.(*query.NumericRangeQuery)
	if !ok {
		t.Fatalf("expected *query.NumericRangeQuery, got %T", q)
	}
	return nq
}

// --- Build() tests ---

func Test_build_empty_string(t *testing.T) {
	if _, ok := Build("").(*query.MatchNoneQuery); !ok {
		t.Fatalf("expected *query.MatchNoneQuery, got %T", Build(""))
	}
}

func Test_build_whitespace_only(t *testing.T) {
	if _, ok := Build("   ").(*query.MatchNoneQuery); !ok {
		t.Fatalf("expected *query.MatchNoneQuery, got %T", Build("   "))
	}
}

func Test_build_bare_wildcard_returns_match_all(t *testing.T) {
	for _, input := range []string{"*", "  *  ", "* *"} {
		if _, ok := Build(input).(*query.MatchAllQuery); !ok {
			t.Fatalf("Build(%q): expected *query.MatchAllQuery, got %T", input, Build(input))
		}
	}
}

func Test_build_ignores_standalone_wildcard_terms(t *testing.T) {
	for _, input := range []string{"* golang", "golang *", "* golang *"} {
		bq := buildBoolQ(t, input)
		clauses := mustClauses(t, bq)
		if len(clauses) != 1 {
			t.Fatalf("Build(%q): expected 1 must clause, got %d", input, len(clauses))
		}
		dq := asDisjunction(t, clauses[0])
		if len(dq.Disjuncts) != 4 {
			t.Fatalf("Build(%q): expected 4 disjuncts, got %d", input, len(dq.Disjuncts))
		}
		for _, d := range dq.Disjuncts {
			if wq, ok := d.(*query.WildcardQuery); ok && wq.Wildcard == "*" {
				t.Fatalf("Build(%q): contains an unbounded wildcard query", input)
			}
		}
	}
}

func Test_build_negated_standalone_wildcard_returns_match_none(t *testing.T) {
	for _, input := range []string{"-*", "golang -*"} {
		if _, ok := Build(input).(*query.MatchNoneQuery); !ok {
			t.Fatalf("Build(%q): expected *query.MatchNoneQuery, got %T", input, Build(input))
		}
	}
}

func Test_remove_standalone_wildcards(t *testing.T) {
	tests := map[string]string{
		"*":               "",
		"* *":             "",
		"* golang":        "golang",
		"golang * docs":   "golang docs",
		"golang*":         "golang*",
		"title:*":         "title:*",
		`"*"`:             "*",
		"(golang|*)":      "",
		"(golang|*) docs": "docs",
		"-*":              "",
		"golang -*":       "",
	}
	for input, want := range tests {
		if got := RemoveStandaloneWildcards(input); got != want {
			t.Errorf("RemoveStandaloneWildcards(%q) = %q, want %q", input, got, want)
		}
	}
}

func Test_build_wildcard_alternative_returns_match_all(t *testing.T) {
	for _, input := range []string{"(golang|*)", "(*|golang)"} {
		if _, ok := Build(input).(*query.MatchAllQuery); !ok {
			t.Fatalf("Build(%q): expected *query.MatchAllQuery, got %T", input, Build(input))
		}
	}
}

func Test_build_ignores_match_all_alternative_in_conjunction(t *testing.T) {
	bq := buildBoolQ(t, "(golang|*) docs")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	dq := asDisjunction(t, clauses[0])
	if len(dq.Disjuncts) != 4 {
		t.Fatalf("expected 4 disjuncts, got %d", len(dq.Disjuncts))
	}
}

func Test_build_simple_word_returns_boolean_query(t *testing.T) {
	bq := buildBoolQ(t, "golang")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	// a plain word fans out to title/text MatchQuery + url/domain WildcardQuery
	dq := asDisjunction(t, clauses[0])
	if len(dq.Disjuncts) != 4 {
		t.Fatalf("expected 4 disjuncts (title, text, url, domain), got %d", len(dq.Disjuncts))
	}
}

func Test_build_negated_word(t *testing.T) {
	bq := buildBoolQ(t, "-golang")
	// Single negated token: Must is nil.
	if bq.Must != nil {
		t.Fatalf("negated-only word: expected Must to be nil, got %T", bq.Must)
	}
	// The base URL boost is for a positive term only; without a Must clause
	// it would be the only required clause.
	if bq.Should != nil {
		t.Fatalf("negated-only word: expected Should to be nil, got %T", bq.Should)
	}
	nots := mustNotClauses(t, bq)
	if len(nots) != 1 {
		t.Fatalf("expected 1 must_not clause, got %d", len(nots))
	}
	asDisjunction(t, nots[0])
}

func Test_build_quoted_phrase(t *testing.T) {
	bq := buildBoolQ(t, `"hello world"`)
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	// The quoted token produces a DisjunctionQuery(title_phrase, text_phrase).
	dq := asDisjunction(t, clauses[0])
	if len(dq.Disjuncts) != 2 {
		t.Fatalf("expected 2 disjuncts (title, text), got %d", len(dq.Disjuncts))
	}
	titlePhrase := asMatchPhrase(t, dq.Disjuncts[0])
	if titlePhrase.MatchPhrase != "hello world" {
		t.Fatalf("expected MatchPhrase %q, got %q", "hello world", titlePhrase.MatchPhrase)
	}
	if titlePhrase.FieldVal != "title" {
		t.Fatalf("expected field %q, got %q", "title", titlePhrase.FieldVal)
	}
}

func Test_build_negated_quoted_phrase(t *testing.T) {
	bq := buildBoolQ(t, `-"hello world"`)
	// Single negated token: Must is nil.
	if bq.Must != nil {
		t.Fatalf("negated-only phrase: expected Must to be nil, got %T", bq.Must)
	}
	nots := mustNotClauses(t, bq)
	if len(nots) != 1 {
		t.Fatalf("expected 1 must_not clause, got %d", len(nots))
	}
	dq := asDisjunction(t, nots[0])
	mpq := asMatchPhrase(t, dq.Disjuncts[0])
	if mpq.MatchPhrase != "hello world" {
		t.Fatalf("expected MatchPhrase %q, got %q", "hello world", mpq.MatchPhrase)
	}
}

func Test_build_title_field(t *testing.T) {
	bq := buildBoolQ(t, "title:golang")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	mq := asMatch(t, clauses[0])
	if mq.Match != "golang" {
		t.Fatalf("expected Match %q, got %q", "golang", mq.Match)
	}
	if mq.FieldVal != "title" {
		t.Fatalf("expected field %q, got %q", "title", mq.FieldVal)
	}
}

func Test_build_quoted_text_fields_use_phrase_query(t *testing.T) {
	tests := []struct {
		field string
		value string
	}{
		{field: "title", value: "for your information"},
		{field: "text", value: "exact phrase"},
	}

	for _, test := range tests {
		t.Run(test.field, func(t *testing.T) {
			bq := buildBoolQ(t, fmt.Sprintf(`%s:"%s"`, test.field, test.value))
			clauses := mustClauses(t, bq)
			if len(clauses) != 1 {
				t.Fatalf("expected 1 must clause, got %d", len(clauses))
			}
			pq := asMatchPhrase(t, clauses[0])
			if pq.MatchPhrase != test.value {
				t.Fatalf("expected phrase %q, got %q", test.value, pq.MatchPhrase)
			}
			if pq.FieldVal != test.field {
				t.Fatalf("expected field %q, got %q", test.field, pq.FieldVal)
			}
		})
	}
}

func Test_build_url_field_uses_term_query(t *testing.T) {
	bq := buildBoolQ(t, "url:https://example.com")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	tq := asTerm(t, clauses[0])
	if tq.Term != "https://example.com" {
		t.Fatalf("expected term %q, got %q", "https://example.com", tq.Term)
	}
	if tq.FieldVal != "url" {
		t.Fatalf("expected field %q, got %q", "url", tq.FieldVal)
	}
}

func Test_build_quoted_url_field(t *testing.T) {
	bq := buildBoolQ(t, `url:"https://example.com"`)
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	tq := asTerm(t, clauses[0])
	if tq.Term != "https://example.com" {
		t.Fatalf("expected term %q, got %q", "https://example.com", tq.Term)
	}
	if tq.FieldVal != "url" {
		t.Fatalf("expected field %q, got %q", "url", tq.FieldVal)
	}
}

func Test_build_url_regexp_matches_quoted_and_unquoted_patterns(t *testing.T) {
	idx := newRankingTestIndex(t, map[string]map[string]string{
		"private": {"url": "https://example.com/private"},
		"login":   {"url": "http://example.com/login"},
		"other":   {"url": "https://example.org/private"},
	})
	for _, input := range []string{
		`url_re:^https?://example\.com/(private|login)$`,
		`url_re:"^https?://example\.com/(private|login)$"`,
	} {
		assertGroupMatches(t, idx, input, []string{"login", "private"})
	}
}

func Test_build_url_regexp_keeps_parenthesized_alternation(t *testing.T) {
	idx := newRankingTestIndex(t, map[string]map[string]string{
		"com":   {"url": "https://example.com/"},
		"org":   {"url": "https://example.org/"},
		"other": {"url": "https://other.test/"},
	})
	assertGroupMatches(t, idx, `url_re:(example\.com|example\.org)`, []string{"com", "org"})
}

func Test_build_validated_rejects_invalid_url_regexp(t *testing.T) {
	if _, err := BuildValidated(`url_re:(unclosed`); !errors.Is(err, ErrInvalidRegexp) {
		t.Fatalf("BuildValidated error = %v, want ErrInvalidRegexp", err)
	}
	if _, err := BuildValidated(`url_re:`); !errors.Is(err, ErrInvalidRegexp) {
		t.Fatalf("BuildValidated empty error = %v, want ErrInvalidRegexp", err)
	}
	tooLong := "url_re:" + strings.Repeat("a", MaxURLRegexpLength+1)
	if _, err := BuildValidated(tooLong); !errors.Is(err, ErrInvalidRegexp) {
		t.Fatalf("BuildValidated long error = %v, want ErrInvalidRegexp", err)
	}
	if _, err := BuildValidated(`(url_re:[|url_re:example)`); !errors.Is(err, ErrInvalidRegexp) {
		t.Fatalf("BuildValidated alternation error = %v, want ErrInvalidRegexp", err)
	}
}

// Test that url:"..." quoted syntax produces a TermQuery (supports spaces in URLs).
func Test_build_url_field_quoted_uses_term_query(t *testing.T) {
	f := "file:///C:/Users/My Documents/notes.txt"
	bq := buildBoolQ(t, fmt.Sprintf(`url:"%s"`, f))
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	tq := asTerm(t, clauses[0])
	if tq.Term != f {
		t.Fatalf("expected term %q, got %q", f, tq.Term)
	}
	if tq.FieldVal != "url" {
		t.Fatalf("expected field %q, got %q", "url", tq.FieldVal)
	}
}

func Test_build_domain_field_uses_term_query(t *testing.T) {
	bq := buildBoolQ(t, "domain:example.com")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	tq := asTerm(t, clauses[0])
	if tq.Term != "example.com" {
		t.Fatalf("expected term %q, got %q", "example.com", tq.Term)
	}
	if tq.FieldVal != "domain" {
		t.Fatalf("expected field %q, got %q", "domain", tq.FieldVal)
	}
}

func Test_build_type_web(t *testing.T) {
	bq := buildBoolQ(t, "type:web")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	nq := asNumericRange(t, clauses[0])
	if nq.FieldVal != "type" {
		t.Fatalf("expected field %q, got %q", "type", nq.FieldVal)
	}
}

func Test_build_type_file(t *testing.T) {
	bq := buildBoolQ(t, "type:file")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	nq := asNumericRange(t, clauses[0])
	if nq.FieldVal != "type" {
		t.Fatalf("expected field %q, got %q", "type", nq.FieldVal)
	}
	if nq.Min == nil || *nq.Min != 1 || nq.Max == nil || *nq.Max != 3 {
		t.Fatalf("expected aggregate file range [1, 3), got min=%v max=%v", nq.Min, nq.Max)
	}
}

func Test_build_type_remote(t *testing.T) {
	bq := buildBoolQ(t, "type:remote")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	nq := asNumericRange(t, clauses[0])
	if nq.FieldVal != "type" {
		t.Fatalf("expected field %q, got %q", "type", nq.FieldVal)
	}
	if nq.Min == nil || *nq.Min != 2 || nq.Max == nil || *nq.Max != 3 {
		t.Fatalf("expected remote range [2, 3), got min=%v max=%v", nq.Min, nq.Max)
	}
}

func Test_build_unknown_type_falls_through(t *testing.T) {
	// Unknown type value: should not panic, treated as a plain word.
	buildBoolQ(t, "type:nonexistent")
}

func Test_build_user_id(t *testing.T) {
	bq := buildBoolQ(t, "user_id:42")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	nq := asNumericRange(t, clauses[0])
	if nq.FieldVal != "user_id" {
		t.Fatalf("expected field %q, got %q", "user_id", nq.FieldVal)
	}
	if nq.Min == nil || *nq.Min != 42 {
		t.Fatalf("expected Min=42, got %v", nq.Min)
	}
	if nq.Max == nil || *nq.Max != 42 {
		t.Fatalf("expected Max=42, got %v", nq.Max)
	}
}

func Test_build_visit_count_range(t *testing.T) {
	bq := buildBoolQ(t, "visits:2..4")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	nq := asNumericRange(t, clauses[0])
	if nq.FieldVal != "add_count" {
		t.Fatalf("expected field %q, got %q", "add_count", nq.FieldVal)
	}
	if nq.Min == nil || *nq.Min != 2 {
		t.Fatalf("expected Min=2, got %v", nq.Min)
	}
	if nq.Max == nil || *nq.Max != 4 {
		t.Fatalf("expected Max=4, got %v", nq.Max)
	}
}

func Test_build_visit_count_open_range(t *testing.T) {
	bq := buildBoolQ(t, "visits:10..")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	nq := asNumericRange(t, clauses[0])
	if nq.FieldVal != "add_count" {
		t.Fatalf("expected field %q, got %q", "add_count", nq.FieldVal)
	}
	if nq.Min == nil || *nq.Min != 10 {
		t.Fatalf("expected Min=10, got %v", nq.Min)
	}
	if nq.Max != nil {
		t.Fatalf("expected nil Max, got %v", nq.Max)
	}
}

func Test_build_relative_time_comparisons(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	tests := []struct {
		field         string
		value         string
		wantMin       *float64
		wantMax       *float64
		wantMinClosed bool
		wantMaxClosed bool
	}{
		{field: "updated", value: ">30s", wantMax: new(float64(2_000_000_000 - 30)), wantMinClosed: true},
		{field: "added", value: ">90d", wantMax: new(float64(2_000_000_000 - 90*24*60*60)), wantMinClosed: true},
		{field: "updated", value: ">=2w", wantMax: new(float64(2_000_000_000 - 2*7*24*60*60)), wantMinClosed: true, wantMaxClosed: true},
		{field: "added", value: "<24h", wantMin: new(float64(2_000_000_000 - 24*60*60)), wantMaxClosed: true},
		{field: "updated", value: "<=30m", wantMin: new(float64(2_000_000_000 - 30*60)), wantMinClosed: true, wantMaxClosed: true},
	}
	for _, test := range tests {
		t.Run(test.field+test.value, func(t *testing.T) {
			q, ok := buildTimeQuery(test.field, test.value, now)
			if !ok {
				t.Fatalf("buildTimeQuery(%q, %q) was not accepted", test.field, test.value)
			}
			nq := asNumericRange(t, q)
			if nq.FieldVal != test.field {
				t.Fatalf("field = %q, want %q", nq.FieldVal, test.field)
			}
			if !equalFloatPointers(nq.Min, test.wantMin) || !equalFloatPointers(nq.Max, test.wantMax) {
				t.Fatalf("range = %v..%v, want %v..%v", nq.Min, nq.Max, test.wantMin, test.wantMax)
			}
			if nq.InclusiveMin == nil || *nq.InclusiveMin != test.wantMinClosed {
				t.Fatalf("inclusive min = %v, want %t", nq.InclusiveMin, test.wantMinClosed)
			}
			if nq.InclusiveMax == nil || *nq.InclusiveMax != test.wantMaxClosed {
				t.Fatalf("inclusive max = %v, want %t", nq.InclusiveMax, test.wantMaxClosed)
			}
		})
	}
}

func Test_build_absolute_date_comparisons(t *testing.T) {
	date := float64(time.Date(2026, time.April, 28, 0, 0, 0, 0, time.UTC).Unix())
	tests := []struct {
		field         string
		value         string
		wantMin       *float64
		wantMax       *float64
		wantMinClosed bool
		wantMaxClosed bool
	}{
		{field: "updated", value: ">2026-04-28", wantMin: &date, wantMaxClosed: true},
		{field: "added", value: ">=2026-04-28", wantMin: &date, wantMinClosed: true, wantMaxClosed: true},
		{field: "updated", value: "<2026-04-28", wantMax: &date, wantMinClosed: true},
		{field: "added", value: "<=2026-04-28", wantMax: &date, wantMinClosed: true, wantMaxClosed: true},
	}
	for _, test := range tests {
		t.Run(test.field+test.value, func(t *testing.T) {
			q, ok := buildTimeQuery(test.field, test.value, time.Time{})
			if !ok {
				t.Fatalf("buildTimeQuery(%q, %q) was not accepted", test.field, test.value)
			}
			nq := asNumericRange(t, q)
			if nq.FieldVal != test.field {
				t.Fatalf("field = %q, want %q", nq.FieldVal, test.field)
			}
			if !equalFloatPointers(nq.Min, test.wantMin) || !equalFloatPointers(nq.Max, test.wantMax) {
				t.Fatalf("range = %v..%v, want %v..%v", nq.Min, nq.Max, test.wantMin, test.wantMax)
			}
			if nq.InclusiveMin == nil || *nq.InclusiveMin != test.wantMinClosed {
				t.Fatalf("inclusive min = %v, want %t", nq.InclusiveMin, test.wantMinClosed)
			}
			if nq.InclusiveMax == nil || *nq.InclusiveMax != test.wantMaxClosed {
				t.Fatalf("inclusive max = %v, want %t", nq.InclusiveMax, test.wantMaxClosed)
			}
		})
	}
}

func equalFloatPointers(got, want *float64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func Test_build_relative_time_filter_is_field_specific(t *testing.T) {
	bq := buildBoolQ(t, "privacy updated:>90d added:>=2026-04-28")
	clauses := mustClauses(t, bq)
	if len(clauses) != 3 {
		t.Fatalf("expected 3 must clauses without phrase wrapping, got %d", len(clauses))
	}
	asDisjunction(t, clauses[0])
	nq := asNumericRange(t, clauses[1])
	if nq.FieldVal != "updated" {
		t.Fatalf("field = %q, want updated", nq.FieldVal)
	}
	nq = asNumericRange(t, clauses[2])
	if nq.FieldVal != "added" {
		t.Fatalf("field = %q, want added", nq.FieldVal)
	}
}

func Test_build_invalid_time_filter_falls_through(t *testing.T) {
	for _, test := range []struct {
		field string
		value string
	}{
		{field: "age", value: ">90d"},
		{field: "added", value: "90d"},
		{field: "updated", value: ">90"},
		{field: "added", value: ">-1d"},
		{field: "updated", value: ">1y"},
		{field: "added", value: ">999999999999999999999d"},
		{field: "updated", value: ">2026-2-01"},
		{field: "added", value: ">2026-02-29"},
		{field: "updated", value: "<2026-04-31"},
		{field: "added", value: ">=2026-04-28T10:30:00Z"},
	} {
		if _, ok := buildTimeQuery(test.field, test.value, time.Now()); ok {
			t.Errorf("buildTimeQuery(%q, %q) was accepted", test.field, test.value)
		}
	}
}

func Test_build_wildcard_word(t *testing.T) {
	bq := buildBoolQ(t, "go*")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	dq := asDisjunction(t, clauses[0])
	urlRegexpCount := 0
	for _, d := range dq.Disjuncts {
		if rq, ok := d.(*query.RegexpQuery); ok {
			urlRegexpCount++
			if rq.FieldVal != "url" || rq.Regexp != `(?i).*go.*` {
				t.Fatalf("unexpected URL regexp query: %#v", rq)
			}
			continue
		}
		wq := asWildcard(t, d)
		if wq.FieldVal == "url" {
			t.Fatalf("URL wildcard was not made case insensitive: %#v", wq)
		}
	}
	if urlRegexpCount != 1 {
		t.Fatalf("URL regexp count = %d, want 1", urlRegexpCount)
	}
}

func Test_build_wildcard_field(t *testing.T) {
	bq := buildBoolQ(t, "title:go*")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	wq := asWildcard(t, clauses[0])
	if wq.Wildcard != "go*" {
		t.Fatalf("expected wildcard %q, got %q", "go*", wq.Wildcard)
	}
	if wq.FieldVal != "title" {
		t.Fatalf("expected field %q, got %q", "title", wq.FieldVal)
	}
}

func Test_build_url_wildcard_is_case_insensitive(t *testing.T) {
	bq := buildBoolQ(t, "url:*README.md*")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	rq := asRegexp(t, clauses[0])
	if rq.Regexp != `(?i).*README\.md.*` {
		t.Fatalf("expected case insensitive regexp %q, got %q", `(?i).*README\.md.*`, rq.Regexp)
	}
	if rq.FieldVal != "url" {
		t.Fatalf("expected field %q, got %q", "url", rq.FieldVal)
	}
}

func Test_build_alternation(t *testing.T) {
	bq := buildBoolQ(t, "(foo|bar)")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	dq := asDisjunction(t, clauses[0])
	if len(dq.Disjuncts) != 2 {
		t.Fatalf("expected 2 disjuncts (foo, bar), got %d", len(dq.Disjuncts))
	}
	// each part is itself a DisjunctionQuery (title/text/url/domain)
	asDisjunction(t, dq.Disjuncts[0])
	asDisjunction(t, dq.Disjuncts[1])
}

func Test_build_metadata_alternation(t *testing.T) {
	bq := buildBoolQ(t, "metadata.type:(toot|tweet)")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	dq := asDisjunction(t, clauses[0])
	if len(dq.Disjuncts) != 2 {
		t.Fatalf("expected 2 disjuncts, got %d", len(dq.Disjuncts))
	}
	for i, want := range []string{"toot", "tweet"} {
		tq := asTerm(t, dq.Disjuncts[i])
		if tq.Term != want {
			t.Errorf("term %d = %q, want %q", i, tq.Term, want)
		}
		if tq.FieldVal != "metadata.type" {
			t.Errorf("field %d = %q, want metadata.type", i, tq.FieldVal)
		}
	}
}

// Test for issue #274: field:(a|b) alternation syntax
func Test_build_domain_alternation(t *testing.T) {
	bq := buildBoolQ(t, "domain:(nogon.eu|docs.nogon.eu)")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	dq := asDisjunction(t, clauses[0])
	if len(dq.Disjuncts) != 2 {
		t.Fatalf("expected 2 disjuncts, got %d", len(dq.Disjuncts))
	}
	tq0 := asTerm(t, dq.Disjuncts[0])
	if tq0.Term != "nogon.eu" {
		t.Fatalf("expected term %q, got %q", "nogon.eu", tq0.Term)
	}
	if tq0.FieldVal != "domain" {
		t.Fatalf("expected field %q, got %q", "domain", tq0.FieldVal)
	}
	tq1 := asTerm(t, dq.Disjuncts[1])
	if tq1.Term != "docs.nogon.eu" {
		t.Fatalf("expected term %q, got %q", "docs.nogon.eu", tq1.Term)
	}
	if tq1.FieldVal != "domain" {
		t.Fatalf("expected field %q, got %q", "domain", tq1.FieldVal)
	}
}

func Test_build_domain_alternation_with_wildcard(t *testing.T) {
	bq := buildBoolQ(t, "domain:(nogon.eu|*.nogon.eu)")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	dq := asDisjunction(t, clauses[0])
	if len(dq.Disjuncts) != 2 {
		t.Fatalf("expected 2 disjuncts, got %d", len(dq.Disjuncts))
	}
	// First part has no wildcard: TermQuery
	tq := asTerm(t, dq.Disjuncts[0])
	if tq.Term != "nogon.eu" {
		t.Fatalf("expected term %q, got %q", "nogon.eu", tq.Term)
	}
	// Second part has wildcard: WildcardQuery
	wq := asWildcard(t, dq.Disjuncts[1])
	if wq.Wildcard != "*.nogon.eu" {
		t.Fatalf("expected wildcard %q, got %q", "*.nogon.eu", wq.Wildcard)
	}
	if wq.FieldVal != "domain" {
		t.Fatalf("expected field %q, got %q", "domain", wq.FieldVal)
	}
}

func Test_build_domain_alternation_single_part(t *testing.T) {
	// A single-part group should be equivalent to a plain domain query.
	bq := buildBoolQ(t, "domain:(nogon.eu)")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	tq := asTerm(t, clauses[0])
	if tq.Term != "nogon.eu" {
		t.Fatalf("expected term %q, got %q", "nogon.eu", tq.Term)
	}
	if tq.FieldVal != "domain" {
		t.Fatalf("expected field %q, got %q", "domain", tq.FieldVal)
	}
}

func Test_build_title_alternation(t *testing.T) {
	bq := buildBoolQ(t, "title:(hello|world)")
	clauses := mustClauses(t, bq)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(clauses))
	}
	dq := asDisjunction(t, clauses[0])
	if len(dq.Disjuncts) != 2 {
		t.Fatalf("expected 2 disjuncts, got %d", len(dq.Disjuncts))
	}
	mq0 := asMatch(t, dq.Disjuncts[0])
	if mq0.Match != "hello" {
		t.Fatalf("expected match %q, got %q", "hello", mq0.Match)
	}
	if mq0.FieldVal != "title" {
		t.Fatalf("expected field %q, got %q", "title", mq0.FieldVal)
	}
	mq1 := asMatch(t, dq.Disjuncts[1])
	if mq1.Match != "world" {
		t.Fatalf("expected match %q, got %q", "world", mq1.Match)
	}
}

func Test_build_multiple_words(t *testing.T) {
	bq := buildBoolQ(t, "foo bar")
	clauses := mustClauses(t, bq)
	if len(clauses) != 2 {
		t.Fatalf("expected 2 must clauses (one per word), got %d", len(clauses))
	}
}

func Test_build_multiple_tokens_positive_and_negative(t *testing.T) {
	bq := buildBoolQ(t, "foo -bar")
	musts := mustClauses(t, bq)
	if len(musts) != 1 {
		t.Fatalf("expected 1 must clause, got %d", len(musts))
	}
	nots := mustNotClauses(t, bq)
	if len(nots) != 1 {
		t.Fatalf("expected 1 must_not clause, got %d", len(nots))
	}
}

// Test that all-field-specific multi-token queries skip the phrase-query wrapping.
// The Must clause should contain the token queries directly (via ConjunctionQuery),
// NOT wrapped in a DisjunctionQuery with a phrase query arm.

func Test_build_all_field_specific_url_user_id(t *testing.T) {
	bq := buildBoolQ(t, `url:"https://example.com" user_id:5`)
	clauses := mustClauses(t, bq)
	// Should have 2 clauses directly (url TermQuery + user_id NumericRange),
	// not 1 outer DisjunctionQuery.
	if len(clauses) != 2 {
		t.Fatalf("expected 2 must clauses (no phrase wrap), got %d", len(clauses))
	}
	asTerm(t, clauses[0])
	asNumericRange(t, clauses[1])
}

func Test_build_all_field_specific_domain_title(t *testing.T) {
	bq := buildBoolQ(t, "domain:example.com title:foo")
	clauses := mustClauses(t, bq)
	if len(clauses) != 2 {
		t.Fatalf("expected 2 must clauses (no phrase wrap), got %d", len(clauses))
	}
	asTerm(t, clauses[0])
	asMatch(t, clauses[1])
}

// A mixed query (one field-specific, one free text) should also skip phrase wrapping
// because the presence of any field-specific token makes a full-string phrase query wrong.
func Test_build_mixed_field_and_free_text_skips_phrase_wrap(t *testing.T) {
	bq := buildBoolQ(t, "python domain:example.com")
	clauses := mustClauses(t, bq)
	// Should have 2 clauses directly (free-text DisjunctionQuery + domain TermQuery),
	// not 1 outer DisjunctionQuery wrapping a phrase.
	if len(clauses) != 2 {
		t.Fatalf("expected 2 must clauses (no phrase wrap), got %d", len(clauses))
	}
	asDisjunction(t, clauses[0]) // free-text "python" fans out to title/text/url/domain
	asTerm(t, clauses[1])        // domain:example.com → TermQuery
}

// --- normalizeFileURL tests ---

func Test_normalizeFileURL(t *testing.T) {
	cases := []struct {
		input     string
		wantExact string
		wantHas   []string
	}{
		{input: "https://example.com/path", wantExact: "https://example.com/path"},
		{input: "*foo", wantExact: "*foo"},
		{input: "/home/user/doc.pdf", wantHas: []string{"file://", "/home/user/doc.pdf"}},
		{input: "docs/file.txt", wantHas: []string{"file://"}},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeFileURL(tc.input)
			if tc.wantExact != "" && got != tc.wantExact {
				t.Fatalf("normalizeFileURL(%q): expected %q, got %q", tc.input, tc.wantExact, got)
			}
			for _, sub := range tc.wantHas {
				if !strings.Contains(got, sub) {
					t.Fatalf("normalizeFileURL(%q): expected result to contain %q, got %q", tc.input, sub, got)
				}
			}
		})
	}
}
