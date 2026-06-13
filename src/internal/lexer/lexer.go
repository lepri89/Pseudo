package lexer

import (
	"strings"
	"unicode"
)

type TokenType string

const (
	TOKEN_WORD    TokenType = "WORD"
	TOKEN_STRING  TokenType = "STRING"
	TOKEN_NUMBER  TokenType = "NUMBER"
	TOKEN_ARROW   TokenType = "ARROW"
	TOKEN_ASSIGN  TokenType = "ASSIGN"
	TOKEN_OP      TokenType = "OP"
	TOKEN_LBRACK  TokenType = "LBRACK"
	TOKEN_RBRACK  TokenType = "RBRACK"
	TOKEN_COMMA   TokenType = "COMMA"
	TOKEN_NEWLINE TokenType = "NEWLINE"
	TOKEN_EOF     TokenType = "EOF"
)

type Token struct {
	Type  TokenType
	Value string
	Line  int
}

type Lexer struct {
	source []rune
	pos    int
	line   int
}

func New(source string) *Lexer {
	return &Lexer{source: []rune(source), pos: 0, line: 1}
}

func (l *Lexer) peek(offset int) rune {
	idx := l.pos + offset
	if idx >= len(l.source) {
		return 0
	}
	return l.source[idx]
}

func (l *Lexer) advance() rune {
	ch := l.source[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
	}
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' {
			l.pos++
		} else {
			break
		}
	}
}

func (l *Lexer) readString(quote rune) Token {
	line := l.line
	l.pos++ // skip opening quote
	var sb strings.Builder
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if ch == quote {
			l.pos++
			return Token{TOKEN_STRING, sb.String(), line}
		}
		if ch == '\\' {
			l.pos++
			if l.pos < len(l.source) {
				esc := l.source[l.pos]
				switch esc {
				case 'n':
					sb.WriteRune('\n')
				case 't':
					sb.WriteRune('\t')
				default:
					sb.WriteRune(esc)
				}
				l.pos++
			}
		} else {
			sb.WriteRune(ch)
			l.pos++
		}
	}
	return Token{TOKEN_STRING, sb.String(), line}
}

func (l *Lexer) readNumber() Token {
	line := l.line
	start := l.pos
	for l.pos < len(l.source) && (unicode.IsDigit(l.source[l.pos]) || l.source[l.pos] == '.') {
		l.pos++
	}
	return Token{TOKEN_NUMBER, string(l.source[start:l.pos]), line}
}

func (l *Lexer) readWord() Token {
	line := l.line
	start := l.pos
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '.' || ch == '@' {
			l.pos++
		} else {
			break
		}
	}
	return Token{TOKEN_WORD, string(l.source[start:l.pos]), line}
}

func (l *Lexer) Tokenize() []Token {
	var tokens []Token

	for l.pos < len(l.source) {
		l.skipWhitespace()
		if l.pos >= len(l.source) {
			break
		}

		ch := l.source[l.pos]

		// comment
		if ch == '#' {
			for l.pos < len(l.source) && l.source[l.pos] != '\n' {
				l.pos++
			}
			continue
		}

		// newline
		if ch == '\n' {
			tokens = append(tokens, Token{TOKEN_NEWLINE, "\n", l.line})
			l.advance()
			continue
		}

		// string
		if ch == '"' || ch == '\'' {
			tokens = append(tokens, l.readString(ch))
			continue
		}

		// arrow →
		if ch == 0x2192 { // →
			tokens = append(tokens, Token{TOKEN_ARROW, "→", l.line})
			l.pos++
			continue
		}

		// arrow ->
		if ch == '-' && l.peek(1) == '>' {
			tokens = append(tokens, Token{TOKEN_ARROW, "→", l.line})
			l.pos += 2
			continue
		}

		// negative number
		if ch == '-' && l.pos+1 < len(l.source) && unicode.IsDigit(l.source[l.pos+1]) {
			l.pos++
			tok := l.readNumber()
			tok.Value = "-" + tok.Value
			tokens = append(tokens, tok)
			continue
		}

		// number
		if unicode.IsDigit(ch) {
			tokens = append(tokens, l.readNumber())
			continue
		}

		// operators
		if ch == '>' || ch == '<' || ch == '!' {
			op := string(ch)
			l.pos++
			if l.pos < len(l.source) && l.source[l.pos] == '=' {
				op += "="
				l.pos++
			}
			tokens = append(tokens, Token{TOKEN_OP, op, l.line})
			continue
		}

		if ch == '=' {
			l.pos++
			if l.pos < len(l.source) && l.source[l.pos] == '=' {
				l.pos++
				tokens = append(tokens, Token{TOKEN_OP, "==", l.line})
			} else {
				tokens = append(tokens, Token{TOKEN_ASSIGN, "=", l.line})
			}
			continue
		}

		if ch == '[' {
			tokens = append(tokens, Token{TOKEN_LBRACK, "[", l.line})
			l.pos++
			continue
		}
		if ch == ']' {
			tokens = append(tokens, Token{TOKEN_RBRACK, "]", l.line})
			l.pos++
			continue
		}
		if ch == ',' {
			tokens = append(tokens, Token{TOKEN_COMMA, ",", l.line})
			l.pos++
			continue
		}

		// word
		if unicode.IsLetter(ch) || ch == '_' {
			tokens = append(tokens, l.readWord())
			continue
		}

		// skip unknown
		l.pos++
	}

	tokens = append(tokens, Token{TOKEN_EOF, "", l.line})
	return tokens
}
