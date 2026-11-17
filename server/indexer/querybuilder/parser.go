package querybuilder

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/asciimoo/nogon/server/indexer/searchschema"
)

type TokenType int

const (
	TokenWord TokenType = iota
	TokenQuoted
	TokenField
	TokenAnd
	TokenOr
	TokenNot
	TokenLeftParen
	TokenRightParen
	TokenEOF
)

type Token struct {
	Type  TokenType
	Value string
	start int
	end   int
}

type Lexer struct {
	input        []rune
	pos          int
	depth        int
	pendingField string
}

func New(input string) *Lexer {
	return &Lexer{input: []rune(input)}
}

func (t Token) String() string {
	return fmt.Sprintf("[%d: %q]", t.Type, t.Value)
}

func (l *Lexer) NextToken() (Token, error) {
	for l.pos < len(l.input) && unicode.IsSpace(l.input[l.pos]) {
		l.pos++
	}
	start := l.pos
	if start == len(l.input) {
		return Token{Type: TokenEOF, start: start, end: start}, nil
	}
	field := l.pendingField
	l.pendingField = ""
	var token Token
	var err error
	switch ch := l.input[l.pos]; {
	case ch == '"':
		token, err = l.readQuoted(field == "url_re")
	case field == "url_re":
		token = l.readRawValue(true)
	case ch == '-' && l.pos+1 < len(l.input) && !unicode.IsSpace(l.input[l.pos+1]) && !strings.ContainsRune(")|", l.input[l.pos+1]):
		l.pos++
		token = Token{Type: TokenNot, Value: "-"}
		l.pendingField = field
	case ch == '(':
		l.pos++
		l.depth++
		token = Token{Type: TokenLeftParen, Value: "("}
	case ch == ')':
		l.pos++
		l.depth--
		token = Token{Type: TokenRightParen, Value: ")"}
	case ch == '|':
		l.pos++
		token = Token{Type: TokenOr, Value: "|"}
	case field == "url":
		token = l.readRawValue(false)
	default:
		token = l.readWord(field != "")
	}
	token.start, token.end = start, l.pos
	return token, err
}

func (l *Lexer) readQuoted(regexp bool) (Token, error) {
	start := l.pos
	l.pos++
	var value strings.Builder
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		l.pos++
		if ch == '"' {
			return Token{Type: TokenQuoted, Value: value.String()}, nil
		}
		if ch == '\\' && l.pos < len(l.input) {
			next := l.input[l.pos]
			l.pos++
			if next != '"' && (next != '\\' || regexp) {
				value.WriteRune(ch)
			}
			ch = next
		}
		value.WriteRune(ch)
	}
	return Token{}, diagnostic(Token{start: start}, "unclosed quote; add a closing double quote")
}

// URL and regexp values keep their own parentheses. An unmatched closing
// parenthesis ends the surrounding query group. Pipes in an unquoted regexp
// belong to the regexp; quote the pattern to separate it from a Boolean pipe.
func (l *Lexer) readRawValue(regexp bool) Token {
	var value strings.Builder
	depth := 0
	inClass := false
	for l.pos < len(l.input) && !unicode.IsSpace(l.input[l.pos]) {
		ch := l.input[l.pos]
		if ch == '\\' && l.pos+1 < len(l.input) {
			if regexp {
				value.WriteRune(ch)
			}
			l.pos++
			value.WriteRune(l.input[l.pos])
			l.pos++
			continue
		}
		if regexp && ch == '[' {
			inClass = true
		} else if ch == ']' {
			inClass = false
		}
		if !inClass {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				if depth == 0 && l.depth > 0 {
					break
				}
				depth--
			} else if ch == '|' && depth == 0 && !regexp {
				break
			}
		}
		value.WriteRune(ch)
		l.pos++
	}
	return Token{Type: TokenWord, Value: value.String()}
}

func (l *Lexer) readWord(fieldValue bool) Token {
	var value strings.Builder
	escaped := false
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if unicode.IsSpace(ch) || strings.ContainsRune("()|\"", ch) {
			break
		}
		l.pos++
		if ch == '\\' && l.pos < len(l.input) {
			escaped = true
			value.WriteRune(l.input[l.pos])
			l.pos++
			continue
		}
		if ch == ':' && !fieldValue && !escaped && isQueryField(value.String()) {
			l.pendingField = value.String()
			return Token{Type: TokenField, Value: value.String()}
		}
		value.WriteRune(ch)
	}
	token := Token{Type: TokenWord, Value: value.String()}
	if !escaped && !fieldValue {
		switch token.Value {
		case "AND":
			token.Type = TokenAnd
		case "OR":
			token.Type = TokenOr
		case "NOT":
			token.Type = TokenNot
		}
	}
	return token
}

func isQueryField(name string) bool {
	_, ok := searchschema.Field(name)
	return ok || (strings.HasPrefix(name, "metadata.") && len(name) > len("metadata."))
}

func Tokenize(input string) ([]Token, error) {
	lexer := New(input)
	var tokens []Token
	for {
		token, err := lexer.NextToken()
		if err != nil {
			return tokens, err
		}
		if token.Type == TokenEOF {
			return tokens, nil
		}
		tokens = append(tokens, token)
	}
}
