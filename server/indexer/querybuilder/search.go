package querybuilder

import (
	"strings"

	"github.com/asciimoo/nogon/server/indexer/searchschema"
)

// SearchExpression separates search directives from text matched by Bleve.
type SearchExpression struct {
	Text    string
	Sort    string
	HasSort bool
	source  string
}

// QueryText excludes search directives while keeping source positions intact
// for diagnostics. Text remains the compact form used for semantic input.
func (e SearchExpression) QueryText() string {
	if e.HasSort {
		return e.source
	}
	return e.Text
}

// ParseSearch separates supported search directives while preserving the
// remaining tokens for normal query building. Invalid directives remain
// searchable text.
func ParseSearch(input string) SearchExpression {
	expression := SearchExpression{Text: input}
	tokens, err := Tokenize(input)
	if err != nil {
		return expression
	}

	runes := []rune(input)
	parts := make([]string, 0, len(tokens))
	depth, consumed := 0, 0
	for i, token := range tokens {
		switch token.Type {
		case TokenLeftParen:
			depth++
		case TokenRightParen:
			depth--
		}
		if depth != 0 || (i > 0 && (tokens[i-1].Type == TokenNot || tokens[i-1].Type == TokenField)) {
			continue
		}
		if sortMode, ok := sortDirective(token); ok {
			if before := strings.TrimSpace(string(runes[consumed:token.start])); before != "" {
				parts = append(parts, before)
			}
			consumed = token.end
			expression.Sort = sortMode
			expression.HasSort = true
			for j := token.start; j < token.end; j++ {
				runes[j] = ' '
			}
		}
	}
	if !expression.HasSort {
		return expression
	}

	if after := strings.TrimSpace(string(runes[consumed:])); after != "" {
		parts = append(parts, after)
	}
	expression.Text = strings.Join(parts, " ")
	expression.source = string(runes)
	if strings.TrimSpace(expression.Text) == "" {
		expression.Text = "*"
		expression.source = "*"
	}
	return expression
}

func sortDirective(token Token) (string, bool) {
	if token.Type != TokenWord {
		return "", false
	}
	value, ok := strings.CutPrefix(token.Value, "sort:")
	if !ok {
		return "", false
	}
	definition, ok := searchschema.LookupSort(value)
	if !ok {
		return "", false
	}
	if definition.Default {
		return "", true
	}
	return definition.Value, true
}
