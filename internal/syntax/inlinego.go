package syntax

import (
	"fmt"
	"text/scanner"
)

// scanGo keeps Go tokens opaque to Hyrule layout, including comments, raw
// strings and tabs. Only braces outside Go literals/comments delimit the body.
func scanGo(scan *scanner.Scanner, source []byte, budget int) (string, int, error) {
	start := scan.Position.Offset + 1
	depth := 1
	for count := 0; count < budget; count++ {
		kind := scan.Scan()
		switch kind {
		case scanner.EOF:
			return "", count + 1, fmt.Errorf("unterminated go! block")
		case '{':
			depth++
			if depth > maxDepth {
				return "", count + 1, fmt.Errorf("go! nesting exceeds limit")
			}
		case '}':
			depth--
			if depth == 0 {
				return string(source[start:scan.Position.Offset]), count + 1, nil
			}
		}
	}
	return "", budget, fmt.Errorf("go! block exceeds token limit")
}
