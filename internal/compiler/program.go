// Package compiler owns language-independent rule ordering, reusable patterns
// and OR expansion. Target adapters resolve hooks and validate typed captures;
// matchir owns the resulting ordered decision graph and pure-prefix sharing.
package compiler

import (
	"fmt"
	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
	"slices"
)

// Program holds ordered source rules and their binding declarations.
type Program struct {
	Derivations  []*semantic.Derivation
	API          *semantic.API
	Bindings     map[string]*semantic.Binding
	BindingOrder []*semantic.Binding
	Unions       []*semantic.Union
	Constructors map[string]*semantic.Variant
	Records      []*semantic.Record
	RecordNames  map[string]*semantic.Record
	patterns     map[string]*semantic.Pattern
	rules        []*semantic.Rule
	rule         string
}

// Collect validates names independently of any target-language hook discovery.
func Collect(files []*semantic.File) (*Program, error) {
	program := &Program{Bindings: map[string]*semantic.Binding{}, patterns: map[string]*semantic.Pattern{}, Constructors: map[string]*semantic.Variant{}}
	names := map[string]bool{}
	for _, file := range files {
		for _, item := range file.Items {
			if err := program.collectItem(item, names); err != nil {
				return nil, err
			}
		}
	}
	if err := program.checkNativeNames(); err != nil {
		return nil, err
	}
	if len(program.rules) == 0 && len(program.Derivations) == 0 {
		return nil, fmt.Errorf("ruleset is empty")
	}
	if err := program.collectUnions(); err != nil {
		return nil, err
	}
	if err := program.collectRecords(); err != nil {
		return nil, err
	}
	return program, nil
}

func (program *Program) collectRecords() error {
	program.RecordNames = map[string]*semantic.Record{}
	for _, record := range program.Records {
		if reservedOperation(record.Name) || program.RecordNames[record.Name] != nil || program.Constructors[record.Name] != nil ||
			program.Bindings[record.Name] != nil || program.patterns[record.Name] != nil {
			return &semantic.Diagnostic{Position: record.Position, Message: "duplicate or colliding record " + record.Name}
		}
		program.RecordNames[record.Name] = record
	}
	return nil
}

// Expand checks reusable definitions, then preserves the priority of every
// expanded OR alternative. The adapter validates capture types before expansion.
func (p *Program) Expand(check func(*semantic.Rule) error) ([]*semantic.Rule, error) {
	rules, err := p.reuseRules(slices.Values(p.rules))
	if err != nil {
		return nil, err
	}
	return p.expandRules(slices.Values(rules), check)
}
func (p *Program) fail(x *semantic.Expr, message string) error {
	return &semantic.Diagnostic{Position: x.Position, Rule: p.rule, Message: message}
}

// HasRules distinguishes matching targets from schema-only generation.
func (p *Program) HasRules() bool { return len(p.rules) != 0 || len(p.patterns) != 0 }

func (program *Program) collectItem(item semantic.Item, names map[string]bool) error {
	if item.Derivation != nil {
		program.Derivations = append(program.Derivations, item.Derivation)
	} else if api := item.API; api != nil {
		if program.API != nil {
			return &semantic.Diagnostic{Position: api.Position, Message: "duplicate signature declaration"}
		}
		program.API = api
	} else if b := item.Binding; b != nil {
		if b.Name == "or" {
			return &semantic.Diagnostic{Position: b.Position, Message: "or is reserved for alternatives"}
		}
		if b.Name == "update" {
			return &semantic.Diagnostic{Position: b.Position, Message: "update is reserved for record updates"}
		}
		if _, intrinsic := Arithmetic(b.Name); intrinsic {
			return &semantic.Diagnostic{Position: b.Position, Message: "arithmetic operation is reserved: " + b.Name}
		}
		if program.Bindings[b.Name] != nil {
			return &semantic.Diagnostic{Position: b.Position, Message: "duplicate binding " + b.Name}
		}
		program.Bindings[b.Name] = b
		program.BindingOrder = append(program.BindingOrder, b)
	} else if union := item.Union; union != nil {
		program.Unions = append(program.Unions, union)
	} else if record := item.Record; record != nil {
		program.Records = append(program.Records, record)
	} else if pattern := item.Pattern; pattern != nil {
		if program.patterns[pattern.Name] != nil {
			return &semantic.Diagnostic{Position: pattern.Position, Message: "duplicate pattern " + pattern.Name}
		}
		program.patterns[pattern.Name] = pattern
	} else {
		if names[item.Rule.Name] {
			return &semantic.Diagnostic{Position: item.Rule.Position, Message: "duplicate rule " + item.Rule.Name}
		}
		names[item.Rule.Name] = true
		program.rules = append(program.rules, item.Rule)
	}
	return nil
}
func (program *Program) checkNativeNames() error {
	if program.API != nil {
		for _, name := range []string{"inputs", "outputs"} {
			if program.Bindings[name] != nil || program.patterns[name] != nil {
				return &semantic.Diagnostic{Position: program.API.Position, Message: "native signature reserves " + name}
			}
			for _, union := range program.Unions {
				for _, variant := range union.Variants {
					if variant.Name == name {
						return &semantic.Diagnostic{Position: variant.Position, Message: "native signature reserves " + name}
					}
				}
			}
			for _, record := range program.Records {
				if record.Name == name {
					return &semantic.Diagnostic{Position: record.Position, Message: "native signature reserves " + name}
				}
			}
		}
	}
	return nil
}
