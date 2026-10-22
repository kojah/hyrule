// Package valueir describes resolved value computations. Names, capture scopes,
// source spelling and host type systems have already been resolved. Backends
// supply storage names for the opaque IDs and execute arguments in written order.
package valueir

import "github.com/kojah/lockdoc/tools/hyrule/internal/matchir"

// Kind identifies a value operation, independently of any surface syntax.
type Kind uint8

const (
	Operand Kind = iota
	Project
	Invoke
	Product
	Tagged
	Collection
	Update
)

// MemberID identifies a host-resolved projection path.
type MemberID uint32

// Argument retains written evaluation order and its destination parameter or
// field. Materialize evaluates exactly once into the declared assignment type;
// Validate checks the declared closed value domain at that boundary.
type Argument struct {
	Value       *Value
	Type        matchir.TypeID
	Position    int
	Materialize bool
	Validate    bool
}

// Value is one resolved computation. Record and union schemas retain structural
// information; opaque host calls retain only their hook identity. Update copies
// Base before assigning fields, and never changes its source value.
type Value struct {
	Kind      Kind
	Type      matchir.TypeID
	Operand   matchir.Operand
	Member    MemberID
	Hook      matchir.HookID
	Record    *matchir.RecordSchema
	Union     *matchir.UnionSchema
	Variant   matchir.VariantID
	Base      *Value
	Arguments []Argument
}
