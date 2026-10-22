package goplan

import "go/types"

// DerivationKind selects one validated schema-maintenance operation.
type DerivationKind string

const (
	Freeze    DerivationKind = "freeze"
	Key       DerivationKind = "key"
	Equal     DerivationKind = "equal"
	Visit     DerivationKind = "visit"
	Transform DerivationKind = "transform"
	Display   DerivationKind = "display"
	Store     DerivationKind = "store"
	Load      DerivationKind = "load"
)

// FieldPolicy is a resolved action, not a source-language policy spelling.
type FieldPolicy uint8

const (
	Preserve FieldPolicy = iota
	Clone
	Apply
	Each
	Omit
	ProjectParameter
	CallHook
	ScalarDisplay
)

// DerivedField describes a checked storage action. Destination binds a key field;
// Parameter is a resolved parameter index; Hook is a resolved Go callable.
type DerivedField struct {
	Name        string
	Member      *types.Var
	Policy      FieldPolicy
	Hook        string
	Parameter   int
	Destination *types.Var
	Scalar      *types.Basic
}

// Derivation is a complete typed helper plan. Storage contains the validated
// table/key/ID types; Freeze names the unique owned-publication helper for Store.
type Derivation struct {
	Kind                       DerivationKind
	Function                   string
	Signature                  *types.Signature
	Type                       types.Type
	Fields                     []DerivedField
	Short                      bool
	KeyType, IndexType, IDType types.Type
	Freeze                     string
}
