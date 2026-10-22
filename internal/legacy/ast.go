// Package legacy imports the retired surface syntax for one-way migration.
// Production compilation uses syntax.Parse and never falls back to this parser.
package legacy

import "github.com/kojah/lockdoc/tools/hyrule/internal/syntax"

type Position = syntax.Position
type Diagnostic = syntax.Diagnostic
type Kind = syntax.Kind
type Expr = syntax.Expr
type Binding = syntax.Binding
type Rule = syntax.Rule
type Clause = syntax.Clause
type Pattern = syntax.Pattern
type Item = syntax.Item
type File = syntax.File
type Union = syntax.Union
type Variant = syntax.Variant
type UnionField = syntax.UnionField
type DecisionArm = syntax.DecisionArm
type Record = syntax.Record
type API = syntax.API

const (
	Call                = syntax.Call
	Variable            = syntax.Variable
	Wildcard            = syntax.Wildcard
	Literal             = syntax.Literal
	Alternation         = syntax.Alternation
	Effects             = syntax.Effects
	Query               = syntax.Query
	Symbol              = syntax.Symbol
	Comparison          = syntax.Comparison
	List                = syntax.List
	Update              = syntax.Update
	OrderedAlternatives = syntax.OrderedAlternatives
	Selection           = syntax.Selection
	Pure                = syntax.Pure
	Do                  = syntax.Do
	Decline             = syntax.Decline
)
