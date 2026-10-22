// Package goplan binds portable matching/value plans to resolved Go storage.
// It contains no parser or semantic expressions and performs no name resolution.
package goplan

import (
	"github.com/kojah/lockdoc/tools/hyrule/internal/hostapi"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
	"github.com/kojah/lockdoc/tools/hyrule/internal/valueir"
	"go/types"
)

// Program is the complete Go matching dispatcher handed to source emission.
// Root is nil for a schema-only target. The resolver owns construction; the
// backend must treat all schemas, tables and continuations as immutable.
type Program struct {
	Host    hostapi.Host
	Root    *matchir.Node
	Tables  Tables
	Records []*Record
	Unions  []*Union
	Native  *Native
	Serial  int
}

// Tables gives host meanings to compiler-owned IDs.
type Tables struct {
	Types                    []types.Type
	Members, Hooks, Literals []string
	Expressions              []Expression
	Constants                []*types.Const
}

// Expression is either a pure value computation or a committed continuation.
type Expression struct {
	Value  *valueir.Value
	Result *valueir.Result
}

// Record binds a portable product to Go fields. Views are read-only projections.
type Record struct {
	Schema *matchir.RecordSchema
	Type   types.Type
	Fields []*types.Var
	View   bool
}

// Union binds a portable closed domain to its Go discriminator and payloads.
type Union struct {
	Schema   *matchir.UnionSchema
	Type     types.Type
	Tag      *types.Var
	Variants []*Variant
}

// Variant binds the active fields of one union constructor.
type Variant struct {
	Union  *Union
	ID     matchir.VariantID
	Tag    *types.Const
	Fields []*types.Var
}

// Native describes the optional parameter/result adapter around a recognizer.
type Native struct {
	Original      *types.Signature
	Function      string
	Input, Output *types.Struct
	Total         bool
	Fallback      *valueir.Result
}
