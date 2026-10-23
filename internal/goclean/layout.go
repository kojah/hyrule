package goclean

import (
	"bytes"
	"go/scanner"
	"go/token"
	"strings"
)

// layout removes blank lines left by eliminated scaffolding. Token boundaries
// protect raw strings and embedded Go comments from textual rewriting.
func layout(source []byte) []byte {
	var lexer scanner.Scanner
	files := token.NewFileSet()
	file := files.AddFile("layout.go", -1, len(source))
	lexer.Init(file, source, nil, scanner.ScanComments)
	var output bytes.Buffer
	end, depth := 0, 0
	previous := token.ILLEGAL
	for {
		position, kind, literal := lexer.Scan()
		if kind == token.EOF {
			output.Write(source[end:])
			break
		}
		if kind == token.SEMICOLON && literal == "\n" {
			continue
		}
		start := file.Offset(position)
		gap := string(source[end:start])
		if depth > 0 && strings.Count(gap, "\n") > 1 {
			gap = "\n"
		}
		if (previous == token.LAND || previous == token.LOR || previous == token.PERIOD) && strings.TrimSpace(gap) == "" {
			gap = " "
		}
		output.WriteString(gap)
		length := len(literal)
		if literal == "" {
			length = len(kind.String())
		}
		end = start + length
		output.Write(source[start:end])
		if kind == token.LBRACE {
			depth++
		}
		if kind == token.RBRACE {
			depth--
		}
		previous = kind
	}
	return output.Bytes()
}
