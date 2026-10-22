package semantic

// Derivation requests explicit operations over one complete record schema.
// Field policies are language-level names or host hooks resolved by a backend.
type Derivation struct {
	Position
	Record     string
	Operations []*DerivedOperation
}

// DerivedOperation names a generated function and its explicit host signature.
type DerivedOperation struct {
	Position
	Kind, Function, Signature string
	Fields                    []DerivedField
}

// DerivedField assigns a policy to one logical schema field, including omission.
type DerivedField struct {
	Position
	Name, Policy string
}
