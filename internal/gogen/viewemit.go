package gogen

import (
	"fmt"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/goplan"
	"github.com/kojah/lockdoc/tools/hyrule/internal/matchir"
)

// viewFor returns inline extraction data when a match is a structural view.
func (e *emitter) viewFor(node *matchir.Node) *goplan.ViewHook {
	if node.Kind != matchir.Match || int(node.Hook) >= len(e.tables.Views) {
		return nil
	}
	return e.tables.Views[node.Hook]
}

func (e *emitter) storageName() string {
	e.storage++
	return fmt.Sprintf("hs%d", e.storage)
}

// emitViewMatch inlines one view: the assertion and nil check guard field reads
// scoped to the successful continuation, exactly as the hook literal did.
func (e *emitter) emitViewMatch(node *matchir.Node, view *goplan.ViewHook) error {
	input, name := valueName(node.Input), "_"
	if len(node.Outputs) > 0 || view.Pointer {
		name = e.storageName()
	}
	switch {
	case view.Assert && view.Pointer:
		fmt.Fprintf(&e.out, "if %s, hok := %s.(%s); hok && %s != nil {\n", name, input, view.Type, name)
	case view.Assert:
		fmt.Fprintf(&e.out, "if %s, hok := %s.(%s); hok {\n", name, input, view.Type)
	case view.Pointer:
		fmt.Fprintf(&e.out, "if %s := %s; %s != nil {\n", name, input, name)
	case name != "_":
		fmt.Fprintf(&e.out, "{\n%s := %s\n", name, input)
	default:
		e.out.WriteString("{\n")
	}
	e.emitViewOutputs(node, view, name)
	if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
		return err
	}
	e.out.WriteString("}\n")
	return nil
}

func (e *emitter) emitViewOutputs(node *matchir.Node, view *goplan.ViewHook, storage string) {
	for index, output := range node.Outputs {
		source := storage
		if index < len(view.Fields) {
			source += "." + view.Fields[index]
		}
		fmt.Fprintf(&e.out, "%s := %s\n_ = %s\n", valueName(output), source, valueName(output))
	}
}

// typeRun returns ordered alternatives asserting concrete types on one value.
// Such assertions are pure and mutually exclusive, so a type switch preserves
// first-match order: only alternatives of the input's dynamic type can match.
func (e *emitter) typeRun(node, stop *matchir.Node) []*matchir.Node {
	var run []*matchir.Node
	for next := node; next != nil && next != stop; next = next.Fallback {
		view := e.viewFor(next)
		if view == nil || !view.Assert || !view.Concrete || next.Input != node.Input {
			break
		}
		run = append(run, next)
	}
	return run
}

type typeClause struct {
	types   []string
	pointer bool
	nodes   []*matchir.Node
	binds   bool
	body    string
}

// emitTypeSwitch groups a run by asserted type, keeping each type's
// alternatives in source order. Clauses that bind nothing and emit identical
// bodies share one case; their nil checks compare against each typed nil.
func (e *emitter) emitTypeSwitch(run []*matchir.Node) error {
	var clauses []*typeClause
	byType := map[string]*typeClause{}
	for _, node := range run {
		view := e.viewFor(node)
		clause := byType[view.Type]
		if clause == nil {
			clause = &typeClause{types: []string{view.Type}, pointer: view.Pointer}
			byType[view.Type] = clause
			clauses = append(clauses, clause)
		}
		clause.nodes = append(clause.nodes, node)
		clause.binds = clause.binds || len(node.Outputs) > 0
	}
	input, name := valueName(run[0].Input), e.storageName()
	binds := false
	for _, clause := range clauses {
		body, err := e.capture(func() error { return e.emitClause(clause, name) })
		if err != nil {
			return err
		}
		clause.body, binds = body, binds || clause.binds
	}
	if binds {
		fmt.Fprintf(&e.out, "switch %s := %s.(type) {\n", name, input)
	} else {
		fmt.Fprintf(&e.out, "switch %s.(type) {\n", input)
	}
	for _, clause := range mergeClauses(clauses) {
		fmt.Fprintf(&e.out, "case %s:\n", strings.Join(clause.types, ", "))
		if !clause.pointer {
			e.out.WriteString(clause.body)
			continue
		}
		var checks []string
		for _, typ := range clause.types {
			checks = append(checks, fmt.Sprintf("%s != (%s)(nil)", input, typ))
		}
		if clause.binds {
			checks = []string{name + " != nil"}
		}
		fmt.Fprintf(&e.out, "if %s {\n%s}\n", strings.Join(checks, " && "), clause.body)
	}
	e.out.WriteString("}\n")
	return nil
}

// emitClause writes one type's alternatives. After an alternative that always
// returns, later ones are unreachable but keep their Go type validation.
func (e *emitter) emitClause(clause *typeClause, storage string) error {
	dead := false
	for _, node := range clause.nodes {
		if dead {
			e.out.WriteString("if false { // Unreachable rules retain Go type validation.\n")
		}
		e.emitViewOutputs(node, e.viewFor(node), storage)
		if err := e.emitDecisions(node.Next, node.Fallback); err != nil {
			return err
		}
		if dead {
			e.out.WriteString("}\n")
		}
		dead = dead || continuationReturns(node.Next, node.Fallback)
	}
	return nil
}

func mergeClauses(clauses []*typeClause) []*typeClause {
	var merged []*typeClause
	shared := map[string]*typeClause{}
	for _, clause := range clauses {
		key := fmt.Sprint(clause.pointer, "\x00", clause.body)
		if target := shared[key]; target != nil && !clause.binds {
			target.types = append(target.types, clause.types...)
			continue
		}
		if !clause.binds {
			shared[key] = clause
		}
		merged = append(merged, clause)
	}
	return merged
}

// capture renders nested emission without writing it to the current output.
func (e *emitter) capture(emit func() error) (string, error) {
	saved := e.out
	e.out = strings.Builder{}
	err := emit()
	text := e.out.String()
	e.out = saved
	return text, err
}
