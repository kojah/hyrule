package matchir

import "slices"

// UnionID identifies one closed schema within a compiled program.
type UnionID uint32

// VariantID identifies one exclusive constructor within a union.
type VariantID uint32

// FieldID identifies one active field within a constructor.
type FieldID uint32

// UnionSchema is portable constructor/type knowledge. Storage bindings and tag
// values belong to a backend; every accepted value has exactly one variant.
type UnionSchema struct {
	ID       UnionID
	Name     string
	Type     TypeID
	Variants []VariantSchema
}

// VariantSchema defines one constructor's active fields, in declaration order.
type VariantSchema struct {
	ID     VariantID
	Name   string
	Fields []FieldSchema
}

// FieldSchema describes an active field without a host-language selector.
type FieldSchema struct {
	ID   FieldID
	Name string
	Type TypeID
}

// VariantCase selects an ordered subgraph after establishing one variant.
type VariantCase struct {
	Variant VariantID
	Next    *Node
}

// Variant appends an exclusive constructor test and its typed field extraction.
// Adjacent identical tests share their canonical outputs; different variants
// are never treated as overlapping matcher hooks.
func (cursor *Cursor) Variant(schema *UnionSchema, variant VariantID, input ValueID) []ValueID {
	cursor.check()
	if previous := cursor.previous(); previous != nil && previous.node.Kind == Variant && previous.node.Union == schema &&
		previous.node.Variant == variant && previous.node.Input == input {
		cursor.branch = &previous.children
		return slices.Clone(previous.node.Outputs)
	}
	node := &Node{Kind: Variant, Union: schema, Variant: variant, Input: input, Origin: cursor.origin}
	for _, field := range schema.Variants[variant].Fields {
		node.Outputs = append(node.Outputs, ValueID(len(cursor.graph.types)))
		cursor.graph.types = append(cursor.graph.types, field.Type)
	}
	cursor.append(node)
	return slices.Clone(node.Outputs)
}
