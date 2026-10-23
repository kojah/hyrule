package goresolve

import (
	"fmt"
	"go/constant"
	"go/types"

	"github.com/kojah/lockdoc/tools/hyrule/internal/usefulness"
)

// Value domains of the usefulness space, one per Go type and validation
// context. Heads are canonical so equal tests share one key and arity.

func (s *patternSpace) domain(key string, build func() *usefulness.Domain) *usefulness.Domain {
	if domain := s.domains[key]; domain != nil {
		return domain
	}
	domain := build()
	s.domains[key] = domain
	return domain
}

func (s *patternSpace) canonical(key string, build func() *usefulness.Head) *usefulness.Head {
	if head := s.heads[key]; head != nil {
		return head
	}
	head := build()
	s.heads[key] = head
	return head
}

// unionDomain lists a union's variants. Validated positions admit only the
// declared tags; elsewhere an undeclared tag is a value no variant matches.
func (s *patternSpace) unionDomain(union *boundUnion, closed bool) *usefulness.Domain {
	key := "union:" + union.declaration.Name
	if !closed {
		key += ":open"
	}
	return s.domain(key, func() *usefulness.Domain {
		domain := &usefulness.Domain{}
		basic, _ := union.tag.Type().Underlying().(*types.Basic)
		if !closed && (basic == nil || basic.Info()&types.IsBoolean == 0 || len(union.variants) < 2) {
			domain.Residual = usefulness.Definite
		}
		for _, variant := range union.variants {
			name := variant.declaration.Name
			domain.Heads = append(domain.Heads, &usefulness.Head{Key: "variant:" + name, Label: name, Arity: len(variant.fields),
				Fields: resultNames(variant.signature), Empty: name, Domain: domain})
		}
		return domain
	})
}

// record is the single head of a product schema; matching always succeeds.
func (s *patternSpace) record(record *boundRecord) *usefulness.Head {
	return s.domain("record:"+record.declaration.Name, func() *usefulness.Domain {
		domain := &usefulness.Domain{}
		domain.Heads = []*usefulness.Head{{Key: "record:" + record.declaration.Name, Label: record.declaration.Name,
			Arity: len(record.fields), Fields: resultNames(record.signature), Empty: "_", Domain: domain}}
		return domain
	}).Heads[0]
}

func resultNames(signature *types.Signature) []string {
	names := make([]string, signature.Results().Len()-1)
	for index := range names {
		names[index] = signature.Results().At(index).Name()
	}
	return names
}

// product is a view whose storage is the input struct type itself.
func (s *patternSpace) product(storage types.Type) *usefulness.Head {
	text := types.TypeString(storage, nil)
	return s.domain("struct:"+text, func() *usefulness.Domain {
		domain := &usefulness.Domain{}
		domain.Heads = []*usefulness.Head{{Key: "view:" + text, Label: s.e.typeText(storage), Empty: "_", Domain: domain}}
		return domain
	}).Heads[0]
}

// pointerDomain is closed: nil, or a non-nil pointer whose view reads fields.
func (s *patternSpace) pointerDomain(pointer types.Type) *usefulness.Domain {
	text := types.TypeString(pointer, nil)
	return s.domain("pointer:"+text, func() *usefulness.Domain {
		domain := &usefulness.Domain{}
		label := s.e.typeText(pointer.Underlying().(*types.Pointer).Elem())
		domain.Heads = []*usefulness.Head{{Key: "nil", Label: "nil", Empty: "nil", Domain: domain},
			{Key: "view:" + text, Label: label, Empty: label + "()", Domain: domain}}
		return domain
	})
}

// interfaceDomain has nil and one head per dynamic type a view asserts. Other
// dynamic types and typed nil pointers may occur, so the residual remains.
// The closed implementer set names witnesses; it does not complete the domain.
func (s *patternSpace) interfaceDomain(iface types.Type) *usefulness.Domain {
	return s.domain("interface:"+types.TypeString(iface, nil), func() *usefulness.Domain {
		domain := &usefulness.Domain{Residual: usefulness.Possible}
		domain.Heads = []*usefulness.Head{{Key: "nil", Label: "nil", Empty: "nil", Domain: domain}}
		for _, storage := range s.implementerTypes(iface) {
			domain.Heads = append(domain.Heads, s.implementerHead(domain, storage))
		}
		return domain
	})
}

func (s *patternSpace) implementer(iface, storage types.Type) *usefulness.Head {
	return s.implementerHead(s.interfaceDomain(iface), storage)
}

func (s *patternSpace) implementerHead(domain *usefulness.Domain, storage types.Type) *usefulness.Head {
	text := types.TypeString(storage, nil)
	return s.canonical(fmt.Sprintf("%p|view:%s", domain, text), func() *usefulness.Head {
		named := storage
		if pointer, ok := storage.(*types.Pointer); ok {
			named = pointer.Elem()
		}
		label := s.e.typeText(named)
		return &usefulness.Head{Key: "view:" + text, Label: label, Empty: label + "()", Domain: domain}
	})
}

// implementerTypes lists the storage types derived views may assert from an
// interface: its declared implementer list, or by default the concrete types
// of the interface's package, as the value or else the pointer type.
func (s *patternSpace) implementerTypes(iface types.Type) []types.Type {
	for _, set := range s.e.closed {
		if types.Identical(set.iface, iface) {
			return set.members
		}
	}
	named, ok := types.Unalias(iface).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return nil
	}
	methods := iface.Underlying().(*types.Interface)
	scope := named.Obj().Pkg().Scope()
	var result []types.Type
	for _, name := range scope.Names() {
		object, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || object.IsAlias() || types.IsInterface(object.Type()) || genericStorage(object.Type()) {
			continue
		}
		for _, storage := range []types.Type{object.Type(), types.NewPointer(object.Type())} {
			if types.Implements(storage, methods) {
				result = append(result, storage)
				break
			}
		}
	}
	return result
}

// basicDomain holds literal and typed-constant values of a basic type.
// Booleans are closed. Other types are open: Go does not restrict a named
// type to its declared constants, so those constants only name witnesses.
func (s *patternSpace) basicDomain(t types.Type) *usefulness.Domain {
	return s.domain("basic:"+types.TypeString(t, nil), func() *usefulness.Domain {
		domain := &usefulness.Domain{}
		if basic, _ := t.Underlying().(*types.Basic); basic != nil && basic.Info()&types.IsBoolean != 0 {
			for _, value := range []bool{true, false} {
				text := fmt.Sprint(value)
				domain.Heads = append(domain.Heads, &usefulness.Head{Key: valueKey(constant.MakeBool(value)), Label: text, Empty: text, Domain: domain})
			}
			return domain
		}
		domain.Residual = usefulness.Definite
		named, ok := types.Unalias(t).(*types.Named)
		if !ok || named.Obj().Pkg() == nil {
			return domain
		}
		scope := named.Obj().Pkg().Scope()
		seen := map[string]bool{}
		for _, name := range scope.Names() {
			object, ok := scope.Lookup(name).(*types.Const)
			if !ok || !types.Identical(object.Type(), t) || seen[valueKey(object.Val())] {
				continue
			}
			seen[valueKey(object.Val())] = true
			label := s.constantName(object)
			domain.Heads = append(domain.Heads, &usefulness.Head{Key: valueKey(object.Val()), Label: label, Empty: label, Domain: domain})
		}
		return domain
	})
}

// value returns the head of one constant value in domain.
func (s *patternSpace) value(domain *usefulness.Domain, value constant.Value, label string) *usefulness.Pattern {
	key := valueKey(value)
	for _, head := range domain.Heads {
		if head.Key == key {
			return constructor(head)
		}
	}
	return constructor(s.canonical(fmt.Sprintf("%p|%s", domain, key), func() *usefulness.Head {
		return &usefulness.Head{Key: key, Label: label, Empty: label, Domain: domain}
	}))
}

func valueKey(value constant.Value) string {
	return "value:" + value.Kind().String() + ":" + value.ExactString()
}

func (s *patternSpace) constantName(object *types.Const) string {
	if object.Pkg() == nil || object.Pkg() == s.e.host.Package {
		return object.Name()
	}
	return object.Pkg().Name() + "." + object.Name()
}
