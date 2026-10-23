package goresolve

import (
	"errors"
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/kojah/hyrule/internal/semantic"
	"github.com/kojah/hyrule/internal/usefulness"
)

// patternSpace abstracts typed rule patterns for the usefulness checks. It
// resolves names exactly as matching does, through a private checking
// resolver, so table registrations never reach the published plan.
type patternSpace struct {
	e       *resolver
	domains map[string]*usefulness.Domain
	heads   map[string]*usefulness.Head
	// fields collects, per view head, every Go field or accessor any pattern
	// names; each view occurrence records its own labels until normalization.
	fields      map[*usefulness.Head][]string
	occurrences map[*usefulness.Pattern][]string
	serial      int
}

// checkUsefulness reports rules and written OR alternatives that no input
// reaches, and inputs of an exhaustive input union that no rule surely covers.
func (e *resolver) checkUsefulness(rules []*semantic.Rule, input types.Type) error {
	checker := &resolver{host: e.host, bindings: e.bindings, constructors: e.constructors, unions: e.unions,
		records: e.records, recordNames: maps.Clone(e.recordNames), constants: maps.Clone(e.constants)}
	space := &patternSpace{e: checker, domains: map[string]*usefulness.Domain{}, heads: map[string]*usefulness.Head{},
		fields: map[*usefulness.Head][]string{}, occurrences: map[*usefulness.Pattern][]string{}}
	rows := make([]usefulness.Row, len(rules))
	positions := map[string]semantic.Position{}
	for index, rule := range rules {
		checker.rule, checker.captures = rule.Name, map[string]capture{}
		pattern, err := space.pattern(rule.Match, input, true)
		if err != nil {
			return err
		}
		rows[index] = usefulness.Row{Rule: rule.Name, Guarded: guarded(rule), Pattern: pattern, Choices: rule.Choices}
		positions[rule.Name] = rule.Position
	}
	for index := range rows {
		rows[index].Pattern = space.normalize(rows[index].Pattern)
	}
	var diagnostics []error
	for _, finding := range usefulness.Unreachable(rows) {
		diagnostic := &semantic.Diagnostic{Position: positions[finding.Rule], Rule: finding.Rule,
			Message: "unreachable rule: earlier rules match every input it matches"}
		if finding.Cover != "" {
			diagnostic.Message = "unreachable rule: rule " + finding.Cover + " matches every input it matches"
		}
		if finding.Alternative != nil {
			diagnostic.Position = *finding.Alternative
			diagnostic.Message = "unreachable alternative: earlier rules and alternatives match every input it matches"
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	if union := e.unionFor(input); union != nil && union.declaration.Exhaustive {
		if diagnostic := space.coverage(union, rows); diagnostic != nil {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	if len(diagnostics) == 1 {
		return diagnostics[0]
	}
	return errors.Join(diagnostics...)
}

// guarded reports whether a rule can fail after its pattern matches. Locals
// always evaluate; guards and pattern guards may fall through.
func guarded(rule *semantic.Rule) bool {
	return slices.ContainsFunc(rule.Clauses, func(clause semantic.Clause) bool { return clause.Name == "" })
}

// coverage checks every declared variant of an exhaustive input union. Root
// dispatch rejects undeclared tags, so the variants are the whole domain.
func (s *patternSpace) coverage(union *boundUnion, rows []usefulness.Row) error {
	var missing, unproven []string
	for _, head := range s.unionDomain(union, true).Heads {
		gap, open := usefulness.Missing(rows, &usefulness.Pattern{Kind: usefulness.Constructor, Head: head, Args: wildcards(head.Arity)})
		switch {
		case !open:
		case gap.Definite:
			missing = append(missing, usefulness.Render(gap.Witness))
		default:
			unproven = append(unproven, usefulness.Render(gap.Witness))
		}
	}
	if len(missing) == 0 && len(unproven) == 0 {
		return nil
	}
	var parts []string
	if len(missing) != 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	if len(unproven) != 0 {
		parts = append(parts, "unproven "+strings.Join(unproven, ", ")+
			" (guards, host matchers, accessors and repeated captures are not coverage)")
	}
	message := "non-exhaustive union " + union.declaration.Name + ": " + strings.Join(parts, "; ")
	return &semantic.Diagnostic{Position: union.declaration.Position, Message: message}
}

// pattern abstracts x at type t. Closed values are the root input and the
// fields of closed records and unions, which generated code validates first.
func (s *patternSpace) pattern(x *semantic.Expr, t types.Type, closed bool) (*usefulness.Pattern, error) {
	e := s.e
	x, err := e.constantPattern(x, t)
	if err != nil {
		return nil, err
	}
	repeated := false
	if x.Bind != "" {
		_, repeated = e.captures[x.Bind]
		e.captures[x.Bind] = capture{typ: t}
	}
	result, err := s.shape(x, t, closed)
	if err != nil || !repeated {
		return result, err
	}
	return s.opaque(result), nil
}

func (s *patternSpace) shape(x *semantic.Expr, t types.Type, closed bool) (*usefulness.Pattern, error) {
	e := s.e
	switch x.Kind {
	case semantic.Alternation:
		return s.pattern(x.Args[0], t, closed)
	case semantic.Variable:
		if _, repeated := e.captures[x.Name]; repeated {
			return s.opaque(usefulness.Any), nil
		}
		e.captures[x.Name] = capture{typ: t}
		return usefulness.Any, nil
	case semantic.Literal:
		return s.literal(x.Name, t), nil
	case semantic.Symbol:
		object := e.constants[x.Name].object
		if _, basic := t.Underlying().(*types.Basic); !basic {
			return s.opaque(usefulness.Any), nil // Equality also compares dynamic types.
		}
		return s.value(s.basicDomain(t), object.Val(), s.constantName(object)), nil
	case semantic.Call, semantic.List:
		return s.call(x, t, closed)
	}
	return usefulness.Any, nil
}

// opaque wraps a pattern whose extra test, such as capture equality, the
// space cannot see: it covers nothing and is queried as within.
func (s *patternSpace) opaque(within *usefulness.Pattern) *usefulness.Pattern {
	s.serial++
	return &usefulness.Pattern{Kind: usefulness.Opaque, Head: &usefulness.Head{Key: fmt.Sprintf("opaque#%d", s.serial)}, Within: within}
}

func (s *patternSpace) call(x *semantic.Expr, t types.Type, closed bool) (*usefulness.Pattern, error) {
	e := s.e
	name, sig, indices, err := e.matcher(x, t)
	if err != nil {
		return nil, err
	}
	if x.Kind == semantic.List {
		return s.list(x, t, sig)
	}
	arity := sig.Results().Len() - 1
	var head *usefulness.Head
	view := e.recordNames[x.Name]
	switch constructor := e.constructors[x.Name]; {
	case constructor != nil:
		head = s.unionDomain(constructor.union, closed).Heads[constructor.id]
	case view != nil && !view.declaration.View:
		head = s.record(view)
	default:
		if view == nil {
			if view, err = e.implicitView(x, t); err != nil {
				return nil, err
			}
		}
		if view != nil {
			return s.view(x, view, t, indices)
		}
		closed = false
		head = s.canonical("hook:"+name, func() *usefulness.Head {
			return &usefulness.Head{Key: "hook:" + name, Label: x.Name, Arity: arity, Empty: x.Name}
		})
	}
	pattern := &usefulness.Pattern{Kind: usefulness.Constructor, Head: head, Args: wildcards(arity)}
	if head.Domain == nil {
		pattern.Kind = usefulness.Opaque
	}
	for index, argument := range x.Args {
		child, err := s.pattern(argument, sig.Results().At(indices[index]).Type(), closed)
		if err != nil {
			return nil, err
		}
		pattern.Args[indices[index]] = child
	}
	return pattern, nil
}

// view abstracts a declared or derived view. Its head depends on how the
// storage relates to the input: a product, the non-nil case of a pointer, or
// one dynamic type of an interface. Fields are keyed by Go member name.
func (s *patternSpace) view(x *semantic.Expr, record *boundRecord, t types.Type, indices []int) (*usefulness.Pattern, error) {
	storage := record.typ
	_, pointer := storage.Underlying().(*types.Pointer)
	var head *usefulness.Head
	switch {
	case types.Identical(t, storage) && pointer:
		head = s.pointerDomain(storage).Heads[1]
	case types.Identical(t, storage) && !types.IsInterface(storage):
		head = s.product(storage)
	case types.IsInterface(t) && !types.IsInterface(storage):
		head = s.implementer(t, storage)
	default:
		key := "view:" + types.TypeString(storage, nil) + "@" + types.TypeString(t, nil)
		head = s.canonical(key, func() *usefulness.Head { return &usefulness.Head{Key: key, Label: x.Name} })
	}
	pattern := &usefulness.Pattern{Kind: usefulness.Constructor, Head: head}
	if head.Domain == nil {
		pattern.Kind = usefulness.Opaque
	}
	var labels []string
	for index, argument := range x.Args {
		member := record.fields[indices[index]]
		child, err := s.pattern(argument, member.Type(), false)
		if err != nil {
			return nil, err
		}
		// Accessors are host reads: a refutable projection is opaque.
		if strings.HasSuffix(member.Name(), "()") && child.Kind != usefulness.Wild {
			child = s.opaque(child)
		}
		labels = append(labels, member.Name())
		pattern.Args = append(pattern.Args, child)
		if !slices.Contains(s.fields[head], member.Name()) {
			s.fields[head] = append(s.fields[head], member.Name())
		}
	}
	s.occurrences[pattern] = labels
	return pattern, nil
}

// normalize gives every view occurrence its head's full sorted field list,
// filling fields it does not name with wildcards.
func (s *patternSpace) normalize(pattern *usefulness.Pattern) *usefulness.Pattern {
	if pattern.Kind == usefulness.Wild {
		return pattern
	}
	copy := *pattern
	copy.Args = make([]*usefulness.Pattern, len(pattern.Args))
	for index, argument := range pattern.Args {
		copy.Args[index] = s.normalize(argument)
	}
	if pattern.Within != nil {
		copy.Within = s.normalize(pattern.Within)
	}
	labels, view := s.occurrences[pattern]
	if !view {
		return &copy
	}
	fields := s.fields[pattern.Head]
	slices.Sort(fields)
	pattern.Head.Fields, pattern.Head.Arity = fields, len(fields)
	arguments := wildcards(len(fields))
	for index, label := range labels {
		arguments[slices.Index(fields, label)] = copy.Args[index]
	}
	copy.Args = arguments
	return &copy
}

func (s *patternSpace) list(x *semantic.Expr, t types.Type, sig *types.Signature) (*usefulness.Pattern, error) {
	domain := s.domain("list:"+types.TypeString(t, nil), func() *usefulness.Domain {
		// A host sequence's Len may be negative; then no list pattern matches.
		if _, slice := t.Underlying().(*types.Slice); !slice {
			return &usefulness.Domain{List: true, Residual: usefulness.Possible}
		}
		return &usefulness.Domain{List: true}
	})
	elements := x.Args
	if x.Rest {
		elements = elements[:len(elements)-1]
	}
	var args []*usefulness.Pattern
	for index, element := range elements {
		child, err := s.pattern(element, sig.Results().At(index).Type(), false)
		if err != nil {
			return nil, err
		}
		args = append(args, child)
	}
	if !x.Rest {
		return &usefulness.Pattern{Kind: usefulness.Constructor, Head: usefulness.ListHead(domain, len(args), false), Args: args}, nil
	}
	rest, err := s.pattern(x.Args[len(x.Args)-1], t, false)
	if err != nil {
		return nil, err
	}
	switch {
	case rest.Kind == usefulness.Wild:
	case rest.Kind == usefulness.Constructor && rest.Head.Domain == domain:
		// A nested list rest spells a longer list: [a, ..[b, ..c]] is [a, b, ..c].
		args = append(args, rest.Args...)
		return &usefulness.Pattern{Kind: usefulness.Constructor, Head: usefulness.ListHead(domain, len(args), rest.Head.Rest), Args: args}, nil
	default:
		within := &usefulness.Pattern{Kind: usefulness.Constructor, Head: usefulness.ListHead(domain, len(args), true), Args: args}
		return s.opaque(within), nil
	}
	return &usefulness.Pattern{Kind: usefulness.Constructor, Head: usefulness.ListHead(domain, len(args), true), Args: args}, nil
}

// literal abstracts a literal: nil of a pointer or interface, a Boolean, or a
// value of an infinite basic domain. Other literals are opaque equality tests.
func (s *patternSpace) literal(text string, t types.Type) *usefulness.Pattern {
	if text == "nil" {
		switch t.Underlying().(type) {
		case *types.Pointer:
			return constructor(s.pointerDomain(t).Heads[0])
		case *types.Interface:
			return constructor(s.interfaceDomain(t).Heads[0])
		}
		return s.opaque(usefulness.Any)
	}
	if _, basic := t.Underlying().(*types.Basic); !basic {
		return s.opaque(usefulness.Any)
	}
	value := literalValue(text)
	if value.Kind() == constant.Unknown {
		return s.opaque(usefulness.Any)
	}
	return s.value(s.basicDomain(t), value, text)
}

func literalValue(text string) constant.Value {
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	value := constant.MakeUnknown()
	switch {
	case text == "true" || text == "false":
		value = constant.MakeBool(text == "true")
	case strings.HasPrefix(text, `"`):
		value = constant.MakeFromLiteral(text, token.STRING, 0)
	default:
		for _, kind := range []token.Token{token.INT, token.FLOAT, token.IMAG} {
			if value = constant.MakeFromLiteral(text, kind, 0); value.Kind() != constant.Unknown {
				break
			}
		}
	}
	if negative && value.Kind() != constant.Unknown {
		value = constant.UnaryOp(token.SUB, value, 0)
	}
	return value
}

func constructor(head *usefulness.Head) *usefulness.Pattern {
	return &usefulness.Pattern{Kind: usefulness.Constructor, Head: head}
}

func wildcards(count int) []*usefulness.Pattern {
	result := make([]*usefulness.Pattern, count)
	for index := range result {
		result[index] = usefulness.Any
	}
	return result
}
