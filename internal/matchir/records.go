package matchir

import "slices"

// RecordID identifies one product schema within a compiled program.
type RecordID uint32

// ConstantID identifies a typed constant in the frontend's immutable table.
type ConstantID uint32

// ConstantSchema retains constant identity and type without host spelling.
type ConstantSchema struct {
	ID   ConstantID
	Type TypeID
}

// RecordSchema describes complete named fields without a discriminator or
// target-language selectors. Record extraction is total for its established type.
type RecordSchema struct {
	ID     RecordID
	Name   string
	Type   TypeID
	Fields []FieldSchema
}

// Record appends a pure total field extraction. Adjacent identical projections
// share values just as matcher extractions do; no success predicate is needed.
func (cursor *Cursor) Record(schema *RecordSchema, input ValueID) []ValueID {
	cursor.check()
	if previous := cursor.previous(); previous != nil && previous.node.Kind == Record && previous.node.Record == schema && previous.node.Input == input {
		cursor.branch = &previous.children
		return slices.Clone(previous.node.Outputs)
	}
	node := &Node{Kind: Record, Record: schema, Input: input, Origin: cursor.origin}
	for _, field := range schema.Fields {
		node.Outputs = append(node.Outputs, ValueID(len(cursor.graph.types)))
		cursor.graph.types = append(cursor.graph.types, field.Type)
	}
	cursor.append(node)
	return slices.Clone(node.Outputs)
}
