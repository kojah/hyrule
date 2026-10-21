// Package syntax parses Hyrule's declarative patterns. It has no host-language
// or graph dependencies; matching and construction are distinct typed phases.
package syntax

import "fmt"

// Position identifies original rule text; paths are supplied by the caller.
type Position struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Diagnostic is a source-located compilation error.
type Diagnostic struct {
	Position
	Rule    string `json:"rule,omitempty"`
	Message string `json:"message"`
}

func (diagnostic *Diagnostic) Error() string {
	message := diagnostic.Message
	if diagnostic.Rule != "" {
		message = "rule " + diagnostic.Rule + ": " + message
	}
	return fmt.Sprintf("%s:%d:%d: %s", diagnostic.File, diagnostic.Line, diagnostic.Column, message)
}

// Kind distinguishes constructors, captures, ignored inputs and literals.
type Kind uint8

const (
	Call Kind = iota
	Variable
	Wildcard
	Literal
)

// Expr is a pattern or construction expression. Bind names an entire matched
// operation; a variable without children binds or reuses an operand.
type Expr struct {
	Position
	Kind Kind
	Name string
	Bind string
	Args []*Expr
}

// Binding declares a named matcher, builder or guard implemented by Go.
type Binding struct {
	Position
	Kind     string
	Name     string
	GoName   string
	Comments []string
}

// Rule is one ordered match, optional guard and replacement.
type Rule struct {
	Position
	Name     string
	Match    *Expr
	Guard    *Expr
	Build    *Expr
	Comments []string
}

// Item preserves declaration order for formatting.
type Item struct {
	Binding *Binding
	Rule    *Rule
}

// File retains declarations and comments. Files combine in lexical filename
// order; rules within each file retain their source order.
type File struct {
	Items    []Item
	Trailing []string
}
