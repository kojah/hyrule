package hyrule

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kojah/lockdoc/tools/hyrule/internal/legacy"
	"github.com/kojah/lockdoc/tools/hyrule/internal/syntax"
)

// Migrate converts one legacy rules file using declarations from the same target.
// Dependencies are parsed for names and union domains, never included in output.
// The result must parse as equation syntax before it can be returned for writing.
func Migrate(name string, input []byte, dependencies map[string][]byte) ([]byte, error) {
	file, err := legacy.Parse(name, strings.NewReader(string(input)))
	if err != nil {
		if converted, newErr := syntax.Parse(name, strings.NewReader(string(input))); newErr == nil {
			return syntax.Print(converted), nil
		}
		return nil, err
	}
	files, err := migrationInputs(name, file, dependencies)
	if err != nil {
		return nil, err
	}
	scope := migrationScope{operations: map[string]bool{}, variants: map[string]*syntax.Union{}, constructors: map[string]bool{}}
	for _, f := range files {
		scope.collect(f)
	}
	if err := scope.rewriteFile(file); err != nil {
		return nil, err
	}
	output := syntax.Print(file)
	if _, err := syntax.Parse(name, strings.NewReader(string(output))); err != nil {
		var diagnostic *syntax.Diagnostic
		if errors.As(err, &diagnostic) {
			lines := strings.Split(string(output), "\n")
			start := max(0, diagnostic.Line-3)
			end := min(len(lines), diagnostic.Line+2)
			return nil, fmt.Errorf("migrated output: %w\n%s", err, strings.Join(lines[start:end], "\n"))
		}
		return nil, fmt.Errorf("migrated output: %w", err)
	}
	return output, nil
}

type migrationScope struct {
	operations   map[string]bool
	variants     map[string]*syntax.Union
	constructors map[string]bool
}

func (m migrationScope) collect(file *syntax.File) {
	for _, item := range file.Items {
		switch {
		case item.Binding != nil:
			m.operations[item.Binding.Name] = item.Binding.Kind != "const"
			m.constructors[item.Binding.Name] = item.Binding.Kind == "match"
		case item.Record != nil:
			m.operations[item.Record.Name] = true
			m.constructors[item.Record.Name] = true
		case item.Pattern != nil:
			m.operations[item.Pattern.Name] = true
			m.constructors[item.Pattern.Name] = true
		case item.API != nil:
			m.operations["inputs"] = true
			m.operations["outputs"] = true
		case item.Union != nil:
			for _, v := range item.Union.Variants {
				m.operations[v.Name] = true
				m.constructors[v.Name] = true
				m.variants[v.Name] = item.Union
			}
		}
	}
}
func (m migrationScope) rewrite(x *syntax.Expr) error {
	if x == nil {
		return nil
	}
	if x.Kind == syntax.Literal && strings.HasPrefix(x.Name, "`") {
		value, err := strconv.Unquote(x.Name)
		if err != nil {
			return err
		}
		x.Name = strconv.Quote(value)
	}
	if x.Kind == syntax.Variable {
		base, _, _ := strings.Cut(x.Name, ".")
		x.Quoted = m.constructors[base]
	}
	if x.Kind == syntax.Symbol && x.Unparenthesized && m.operations[x.Name] {
		x.Kind = syntax.Call
		x.Unparenthesized = false
	}
	for _, arg := range x.Args {
		if err := m.rewrite(arg); err != nil {
			return err
		}
	}
	for _, arm := range x.Arms {
		if err := m.rewrite(arm.Pattern); err != nil {
			return err
		}
		if err := m.rewrite(arm.Result); err != nil {
			return err
		}
	}
	if x.Kind != syntax.Selection {
		return nil
	}
	return m.completeSelection(x)
}

func (m migrationScope) completeSelection(x *syntax.Expr) error {
	selected := x.Arms[0]
	patterns := []*syntax.Expr{selected.Pattern}
	if selected.Pattern.Kind == syntax.Alternation {
		patterns = selected.Pattern.Args
	}
	used := map[string]bool{}
	for _, pattern := range patterns {
		used[pattern.Name] = true
	}
	var others []*syntax.Expr
	if patterns[0].Kind == syntax.Literal {
		for _, name := range []string{"true", "false"} {
			if !used[name] {
				others = append(others, &syntax.Expr{Position: x.Position, Kind: syntax.Literal, Name: name})
			}
		}
	} else {
		union := m.variants[patterns[0].Name]
		if union == nil {
			return &syntax.Diagnostic{Position: x.Position, Message: "migration needs the target's union declarations for selective query " + patterns[0].Name}
		}
		for _, v := range union.Variants {
			if !used[v.Name] {
				others = append(others, &syntax.Expr{Position: x.Position, Kind: syntax.Call, Name: v.Name})
			}
		}
	}
	// Expand selected ORs into complete case arms; each result receives the same
	// captures as before, and the query still executes exactly once.
	x.Kind = syntax.Query
	x.Arms = nil
	for _, pattern := range patterns {
		x.Arms = append(x.Arms, syntax.DecisionArm{Pattern: pattern, Result: selected.Result})
	}
	for _, pattern := range others {
		x.Arms = append(x.Arms, syntax.DecisionArm{Pattern: pattern, Result: &syntax.Expr{Position: x.Position, Kind: syntax.Decline}})
	}
	return nil
}

func migrationInputs(name string, file *syntax.File, dependencies map[string][]byte) ([]*syntax.File, error) {
	files := []*syntax.File{file}
	paths := make([]string, 0, len(dependencies))
	for path := range dependencies {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		data := dependencies[path]
		if path == name {
			continue
		}
		other, err := legacy.Parse(path, strings.NewReader(string(data)))
		if err != nil {
			other, err = syntax.Parse(path, strings.NewReader(string(data)))
		}
		if err != nil {
			return nil, err
		}
		files = append(files, other)
	}
	return files, nil
}

func (m migrationScope) rewriteFile(file *syntax.File) error {
	for _, item := range file.Items {
		var roots []*syntax.Expr
		switch {
		case item.Rule != nil:
			item.Rule.Family = "rewrite"
			roots = append(roots, item.Rule.Match, item.Rule.Build)
			for _, clause := range item.Rule.Clauses {
				roots = append(roots, clause.Value)
			}
		case item.Pattern != nil:
			roots = append(roots, item.Pattern.Body)
		case item.API != nil:
			roots = append(roots, item.API.Fallback)
		case item.Binding != nil:
			roots = append(roots, item.Binding.Body)
		}
		for _, root := range roots {
			if err := m.rewrite(root); err != nil {
				return err
			}
		}
	}
	return nil
}
