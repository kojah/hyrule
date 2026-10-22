package syntax

import (
	"io"
	"strings"
	"text/scanner"
	"unicode"
)

const maxDepth = 64
const maxTokens = 100000

type token struct {
	Position
	text   string
	kind   rune
	indent int
}

// lex retains physical newlines and columns. The parser owns layout because
// continuation indentation is distinct from a do/case/schema block.
func lex(name string, input io.Reader) ([]token, error) {
	var scan scanner.Scanner
	scan.Init(input)
	scan.Filename = name
	scan.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanFloats | scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanComments
	scan.Whitespace = 1<<' ' | 1<<'\r'
	var failure error
	scan.Error = func(s *scanner.Scanner, message string) {
		if failure == nil {
			failure = &Diagnostic{Position: Position{name, s.Line, s.Column}, Message: message}
		}
	}
	var result []token
	indent, line := 0, 0
	for len(result) <= maxTokens && failure == nil {
		kind := scan.Scan()
		pos := Position{name, scan.Line, scan.Column}
		text := scan.TokenText()
		if kind == '\t' {
			return nil, &Diagnostic{Position: pos, Message: "tabs are not permitted; use spaces"}
		}
		if pos.Line != line && kind != '\n' && kind != scanner.Comment {
			indent, line = pos.Column-1, pos.Line
		}
		if kind == scanner.RawString {
			value := strings.TrimSuffix(strings.TrimPrefix(text, "`"), "`")
			if value == "" || strings.ContainsAny(value, "\n\r") {
				return nil, &Diagnostic{Position: pos, Message: "expected a backtick-escaped identifier"}
			}
			for i, r := range value {
				if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
					return nil, &Diagnostic{Position: pos, Message: "invalid escaped identifier"}
				}
			}
		}
		result = append(result, token{Position: pos, text: text, kind: kind, indent: indent})
		if kind == scanner.EOF {
			return combineTokens(result), failure
		}
	}
	if failure == nil {
		failure = &Diagnostic{Position: Position{name, scan.Line, scan.Column}, Message: "rule file exceeds token limit"}
	}
	return nil, failure
}

// combineTokens recognizes punctuation without confusing spaced unary minus
// with the query bind arrow, or a guard bar with ordered choice.
func combineTokens(input []token) []token {
	out := make([]token, 0, len(input))
	for i := 0; i < len(input); i++ {
		t := input[i]
		for _, op := range []string{"<|>", "<-", "->", ">=", "<=", "==", "!=", "&&"} {
			if t.text != op[:1] || i+len(op) > len(input) {
				continue
			}
			matches := true
			for j := 1; j < len(op); j++ {
				next := input[i+j]
				if next.Line != t.Line || next.Column != t.Column+j || next.text != op[j:j+1] {
					matches = false
					break
				}
			}
			if matches {
				t.text = op
				i += len(op) - 1
				break
			}
		}
		out = append(out, t)
	}
	return out
}
