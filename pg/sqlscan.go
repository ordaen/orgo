package pg

import (
	"strings"
)

// containsDropTable reports whether the SQL has a DROP TABLE or DROP FOREIGN TABLE statement.
//
// The keywords match in any case, separated by any whitespace and comments. Comments and double quoted identifiers
// are skipped. String literals and dollar quoted strings are scanned as SQL too, because a DO block or an EXECUTE runs
// them, so a data string mentioning DROP TABLE is also reported.
func containsDropTable(sql string) bool {
	found := false
	var prev []string // the words before the current one, reset by other tokens
	scanSQL(sql, func(tok sqlToken) bool {
		switch tok.kind {
		case sqlWord:
			prev = append(prev, tok.text)
			n := len(prev)
			if tok.text == "table" && n >= 2 &&
				(prev[n-2] == "drop" || n >= 3 && prev[n-2] == "foreign" && prev[n-3] == "drop") {
				found = true
			}
		case sqlString:
			found = containsDropTable(tok.text)
			prev = prev[:0]
		default:
			prev = prev[:0]
		}
		return !found
	})
	return found
}

type sqlTokenKind int

const (
	sqlWord   sqlTokenKind = iota // a keyword or an unquoted identifier, lower cased
	sqlString                     // the content of a string literal or a dollar quoted string
	sqlOther                      // a quoted identifier, a number, an operator or a punctuation character
)

type sqlToken struct {
	kind sqlTokenKind
	text string
}

// scanSQL calls emit for the tokens of the SQL, following the PostgreSQL lexical rules for comments, strings and
// identifiers, until emit returns false. Whitespace and comments are skipped. An unterminated comment, string or
// identifier runs to the end of the SQL.
func scanSQL(sql string, emit func(sqlToken) bool) {
	for i := 0; i < len(sql); {
		c := sql[i]
		var tok sqlToken
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
			continue
		case strings.HasPrefix(sql[i:], "--"):
			if j := strings.IndexByte(sql[i:], '\n'); j >= 0 {
				i += j + 1
			} else {
				i = len(sql)
			}
			continue
		case strings.HasPrefix(sql[i:], "/*"):
			i = skipBlockComment(sql, i)
			continue
		case c == '\'':
			tok.kind = sqlString
			tok.text, i = scanQuoted(sql, i, '\'', false)
		case (c == 'e' || c == 'E') && i+1 < len(sql) && sql[i+1] == '\'':
			// E'...' strings have backslash escapes
			tok.kind = sqlString
			tok.text, i = scanQuoted(sql, i+1, '\'', true)
		case c == '"':
			tok.kind = sqlOther
			_, i = scanQuoted(sql, i, '"', false)
		case c == '$':
			if tag, ok := dollarTag(sql, i); ok {
				tok.kind = sqlString
				start := i + len(tag)
				if j := strings.Index(sql[start:], tag); j >= 0 {
					tok.text, i = sql[start:start+j], start+j+len(tag)
				} else {
					tok.text, i = sql[start:], len(sql)
				}
			} else {
				// a parameter like $1
				tok.kind = sqlOther
				i++
				for i < len(sql) && isDigit(sql[i]) {
					i++
				}
			}
		case isIdentStart(c):
			j := i + 1
			for j < len(sql) && isIdentChar(sql[j]) {
				j++
			}
			tok.kind, tok.text, i = sqlWord, strings.ToLower(sql[i:j]), j
		default:
			tok.kind, tok.text, i = sqlOther, sql[i:i+1], i+1
		}
		if !emit(tok) {
			return
		}
	}
}

// skipBlockComment returns the position after the block comment starting at i. Block comments nest.
func skipBlockComment(sql string, i int) int {
	depth := 0
	for i < len(sql) {
		switch {
		case strings.HasPrefix(sql[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(sql[i:], "*/"):
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return i
}

// scanQuoted returns the unescaped content of the string or identifier quoted with q starting at i, and the position
// after it. A doubled quote is a quote, with backslashes a backslash escapes the next character.
func scanQuoted(sql string, i int, q byte, backslashes bool) (string, int) {
	var b strings.Builder
	for i++; i < len(sql); i++ {
		c := sql[i]
		switch {
		case backslashes && c == '\\' && i+1 < len(sql):
			i++
			b.WriteByte(sql[i])
		case c == q && i+1 < len(sql) && sql[i+1] == q:
			i++
			b.WriteByte(q)
		case c == q:
			return b.String(), i + 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), i
}

// dollarTag returns the dollar quote tag starting at i, like $$ or $body$. A $ after an identifier character is part
// of the identifier, and a tag cannot start with a digit, so $1 is a parameter.
func dollarTag(sql string, i int) (string, bool) {
	if i > 0 && isIdentChar(sql[i-1]) {
		return "", false
	}
	j := i + 1
	if j < len(sql) && isDigit(sql[j]) {
		return "", false
	}
	for j < len(sql) && isIdentChar(sql[j]) && sql[j] != '$' {
		j++
	}
	if j < len(sql) && sql[j] == '$' {
		return sql[i : j+1], true
	}
	return "", false
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
