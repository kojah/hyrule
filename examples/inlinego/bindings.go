// Package inlinego demonstrates inline Go calculations and result construction.
package inlinego

import "strings"

// Request is the input matched by the rule.
type Request struct {
	Text string
	A, B int
}

// Plan is a complete output value.
type Plan struct {
	Text  string
	Total int
}

// NewRequest normalizes input before it enters the rules.
func NewRequest(text string, a, b int) Request {
	return Request{Text: strings.TrimSpace(text), A: a, B: b}
}
