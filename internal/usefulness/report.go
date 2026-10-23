package usefulness

import (
	"slices"

	"github.com/kojah/lockdoc/tools/hyrule/internal/semantic"
)

// Row is one OR-free rule expansion, in source order. Consecutive rows with
// the same Rule are the expansions of one rule's OR alternatives.
type Row struct {
	Rule string
	// Guarded rows may fail after matching, so they never cover later rows.
	Guarded bool
	Pattern *Pattern
	// Choices are the written OR alternatives this expansion takes.
	Choices []semantic.Position
}

// Finding is a rule, or one written OR alternative of a reachable rule, that
// matches no input an earlier unguarded row leaves unmatched.
type Finding struct {
	Rule        string
	Alternative *semantic.Position
	// Cover names one earlier rule that alone matches everything this does.
	Cover string
}

// Unreachable reports dead rules and dead written alternatives. A row is
// dead only when the solver proves every input it matches is matched first.
func Unreachable(rows []Row) []Finding {
	var covering [][]*Pattern
	var owners []string
	useful := make([]bool, len(rows))
	before := make([]int, len(rows)) // Covering rows preceding each row.
	for index, row := range rows {
		_, useful[index] = newSolver(possibly).useful(covering, []*Pattern{row.Pattern})
		before[index] = len(covering)
		if !row.Guarded {
			covering = append(covering, []*Pattern{row.Pattern})
			owners = append(owners, row.Rule)
		}
	}
	var findings []Finding
	for start := 0; start < len(rows); {
		end := start
		for end < len(rows) && rows[end].Rule == rows[start].Rule {
			end++
		}
		group, live := rows[start:end], useful[start:end]
		if !slices.Contains(live, true) {
			cover := coveringRule(group, covering[:before[start]], owners)
			findings = append(findings, Finding{Rule: group[0].Rule, Cover: cover})
		} else {
			findings = append(findings, deadAlternatives(group, live)...)
		}
		start = end
	}
	return findings
}

// coveringRule names the first earlier rule whose rows alone cover group.
func coveringRule(group []Row, covering [][]*Pattern, owners []string) string {
	for start := 0; start < len(covering); {
		end := start
		for end < len(covering) && owners[end] == owners[start] {
			end++
		}
		covered := true
		for _, row := range group {
			if _, ok := newSolver(possibly).useful(covering[start:end], []*Pattern{row.Pattern}); ok {
				covered = false
				break
			}
		}
		if covered {
			return owners[start]
		}
		start = end
	}
	return ""
}

// deadAlternatives reports each written alternative all of whose expansions
// are dead, in the order the rule's expansions first take them.
func deadAlternatives(group []Row, live []bool) []Finding {
	var order []semantic.Position
	reachable := map[semantic.Position]bool{}
	for index, row := range group {
		for _, choice := range row.Choices {
			if _, seen := reachable[choice]; !seen {
				order = append(order, choice)
			}
			reachable[choice] = reachable[choice] || live[index]
		}
	}
	var findings []Finding
	for _, choice := range order {
		if !reachable[choice] {
			findings = append(findings, Finding{Rule: group[0].Rule, Alternative: &choice})
		}
	}
	return findings
}

// Gap is an input that no unguarded row surely matches.
type Gap struct {
	Witness *Pattern
	// Definite is set when no row can match the witness, even guarded rows
	// whose guards succeed or opaque tests that accept it.
	Definite bool
}

// Missing reports whether rows leave an input matching query unmatched.
// Query must not contain opaque tests.
func Missing(rows []Row, query *Pattern) (Gap, bool) {
	var strict, loose [][]*Pattern
	for _, row := range rows {
		loose = append(loose, []*Pattern{row.Pattern})
		if !row.Guarded {
			strict = append(strict, []*Pattern{row.Pattern})
		}
	}
	witness, ok := newSolver(possibly).useful(strict, []*Pattern{query})
	if !ok {
		return Gap{}, false
	}
	if definite, ok := newSolver(surely).useful(loose, []*Pattern{query}); ok {
		return Gap{Witness: definite[0], Definite: true}, true
	}
	return Gap{Witness: witness[0]}, true
}
