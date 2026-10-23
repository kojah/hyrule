package usefulness

// mode selects which side of an uncertain answer the solver takes.
type mode uint8

const (
	// possibly over-approximates usefulness: earlier rows cover only what
	// they surely match, so "useless" is a proof that the query is dead.
	possibly mode = iota
	// surely under-approximates usefulness: earlier rows may match whatever
	// they might, so "useful" is a proof that its witness is uncovered.
	surely
)

// stepBudget bounds one query. An exhausted solver answers conservatively:
// possibly useful, and never surely useful.
const stepBudget = 1 << 16

type solver struct {
	mode      mode
	steps     int
	exhausted bool
}

func newSolver(m mode) *solver { return &solver{mode: m, steps: stepBudget} }

// useful reports whether vector q matches a value no row of rows matches,
// with a witness vector when it does (Maranget's U and I combined).
func (s *solver) useful(rows [][]*Pattern, q []*Pattern) ([]*Pattern, bool) {
	if s.exhausted || s.steps == 0 {
		s.exhausted = true
		return wilds(len(q)), s.mode == possibly
	}
	s.steps--
	if len(q) == 0 {
		return nil, len(rows) == 0
	}
	first := q[0]
	switch first.Kind {
	case Constructor:
		if first.Head.Domain.List {
			return s.list(rows, q)
		}
		return s.specialized(rows, q, first.Head, first.Args)
	case Opaque:
		if s.mode == surely {
			return nil, false // The opaque test might never succeed.
		}
		// Rows applying the same deterministic test, or any value, may cover it.
		if _, ok := s.specialized(rows, q, first.Head, first.Args); !ok {
			return nil, false
		}
		within := first.Within
		if within == nil {
			within = Any
		}
		return s.useful(rows, prepend([]*Pattern{within}, q[1:]))
	default:
		return s.wild(rows, q)
	}
}

// specialized is U(S(head, rows), args ++ q[1:]); the witness rebuilds head.
func (s *solver) specialized(rows [][]*Pattern, q []*Pattern, head *Head, args []*Pattern) ([]*Pattern, bool) {
	var next [][]*Pattern
	for _, row := range rows {
		if specialized, ok := s.specialize(row, head); ok {
			next = append(next, specialized)
		}
	}
	witness, ok := s.useful(next, prepend(args, q[1:]))
	if !ok {
		return nil, false
	}
	arity := len(args)
	built := &Pattern{Kind: Constructor, Head: head, Args: witness[:arity]}
	if head.Domain == nil {
		built.Kind = Opaque
	}
	return prepend([]*Pattern{built}, witness[arity:]), true
}

// specialize keeps a row that can match head, replacing its first column by
// head's arguments. A row whose first test the solver cannot relate to head
// is dropped when it must surely match and kept as a wildcard when it may.
func (s *solver) specialize(row []*Pattern, head *Head) ([]*Pattern, bool) {
	first := row[0]
	switch {
	case first.Kind == Wild:
		return prepend(wilds(head.Arity), row[1:]), true
	case first.Head.Key == head.Key && first.Head.Domain == head.Domain:
		return prepend(first.Args, row[1:]), true
	case s.mode == surely && (first.Kind == Opaque || first.Head.Domain != head.Domain):
		return prepend(wilds(head.Arity), row[1:]), true
	}
	return nil, false
}

// wild handles a wildcard query column: split over a complete set of heads,
// otherwise continue with the rows that match any value (the default matrix).
func (s *solver) wild(rows [][]*Pattern, q []*Pattern) ([]*Pattern, bool) {
	domain, present := s.column(rows)
	if domain == nil {
		witness, ok := s.useful(s.defaults(rows, nil), q[1:])
		if !ok {
			return nil, false
		}
		return prepend([]*Pattern{Any}, witness), true
	}
	if domain.List && (domain.Residual == Closed || s.mode == surely) {
		return s.list(rows, q)
	}
	if domain.List {
		witness, ok := s.useful(s.defaults(rows, domain), q[1:])
		if !ok {
			return nil, false
		}
		return prepend([]*Pattern{{Kind: Other}}, witness), true
	}
	var missing *Head
	for _, head := range domain.Heads {
		if !present[head.Key] {
			missing = head
			break
		}
	}
	residual := domain.Residual
	if s.mode == surely && residual == Possible && missing == nil {
		residual = Closed // Only the present heads surely have values.
	}
	if missing == nil && residual == Closed {
		heads := domain.Heads
		if len(heads) == 0 || s.mode == surely && domain.Residual == Possible {
			heads = s.presentHeads(rows, domain)
		}
		for _, head := range heads {
			if witness, ok := s.specialized(rows, q, head, wilds(head.Arity)); ok {
				return witness, true
			}
		}
		return nil, false
	}
	witness, ok := s.useful(s.defaults(rows, domain), q[1:])
	if !ok {
		return nil, false
	}
	first := &Pattern{Kind: Other}
	if missing != nil {
		first = &Pattern{Kind: Constructor, Head: missing, Args: wilds(missing.Arity)}
	}
	return prepend([]*Pattern{first}, witness), true
}

// column returns the domain of the first column's constructors and the keys
// present. Opaque heads never contribute to completeness.
func (s *solver) column(rows [][]*Pattern) (*Domain, map[string]bool) {
	var domain *Domain
	present := map[string]bool{}
	for _, row := range rows {
		first := row[0]
		if first.Kind != Constructor {
			continue
		}
		if domain == nil {
			domain = first.Head.Domain
		}
		if first.Head.Domain == domain {
			present[first.Head.Key] = true
		}
	}
	return domain, present
}

func (s *solver) presentHeads(rows [][]*Pattern, domain *Domain) []*Head {
	var heads []*Head
	seen := map[string]bool{}
	for _, row := range rows {
		if first := row[0]; first.Kind == Constructor && first.Head.Domain == domain && !seen[first.Head.Key] {
			seen[first.Head.Key] = true
			heads = append(heads, first.Head)
		}
	}
	return heads
}

// defaults is Maranget's default matrix: rows whose first column matches a
// value outside every present head.
func (s *solver) defaults(rows [][]*Pattern, domain *Domain) [][]*Pattern {
	var next [][]*Pattern
	for _, row := range rows {
		first := row[0]
		if first.Kind == Wild || s.mode == surely && (first.Kind == Opaque || first.Head.Domain != domain) {
			next = append(next, row[1:])
		}
	}
	return next
}
