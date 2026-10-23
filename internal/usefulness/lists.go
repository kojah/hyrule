package usefulness

import "strconv"

// ListHead returns the canonical head of a list pattern in domain: exactly
// length elements, or at least length elements with a rest pattern.
func ListHead(domain *Domain, length int, rest bool) *Head {
	key := "len:" + strconv.Itoa(length)
	if rest {
		key = "rest:" + strconv.Itoa(length)
	}
	return &Head{Key: key, Arity: length, Domain: domain, Length: length, Rest: rest}
}

// list splits a sequence column into the exact lengths 0..L and "at least
// L+1", where L is the longest prefix a list pattern in the column names.
// Lengths beyond L behave alike: no pattern constrains their extra elements.
func (s *solver) list(rows [][]*Pattern, q []*Pattern) ([]*Pattern, bool) {
	var domain *Domain
	longest := 0
	for _, pattern := range append(firsts(rows), q[0]) {
		if pattern.Kind == Constructor && pattern.Head.Domain.List {
			if domain == nil {
				domain = pattern.Head.Domain
			}
			longest = max(longest, pattern.Head.Length)
		}
	}
	for length := 0; length <= longest+1; length++ {
		shape := ListHead(domain, length, length == longest+1)
		args := wilds(length)
		if q[0].Kind == Constructor {
			var ok bool
			if args, ok = elements(q[0], shape); !ok {
				continue
			}
		}
		var next [][]*Pattern
		for _, row := range rows {
			if specialized, ok := s.listRow(row, shape); ok {
				next = append(next, specialized)
			}
		}
		if witness, ok := s.useful(next, prepend(args, q[1:])); ok {
			built := &Pattern{Kind: Constructor, Head: shape, Args: witness[:length]}
			return prepend([]*Pattern{built}, witness[length:]), true
		}
	}
	return nil, false
}

func (s *solver) listRow(row []*Pattern, shape *Head) ([]*Pattern, bool) {
	first := row[0]
	switch {
	case first.Kind == Wild:
		return prepend(wilds(shape.Length), row[1:]), true
	case first.Kind == Constructor && first.Head.Domain == shape.Domain:
		if args, ok := elements(first, shape); ok {
			return prepend(args, row[1:]), true
		}
		return nil, false
	case s.mode == surely:
		return prepend(wilds(shape.Length), row[1:]), true
	}
	return nil, false
}

// elements gives a list pattern's element patterns for the values of shape,
// or false when no value of that shape matches it.
func elements(pattern *Pattern, shape *Head) ([]*Pattern, bool) {
	head := pattern.Head
	if !head.Rest {
		return pattern.Args, !shape.Rest && head.Length == shape.Length
	}
	if head.Length > shape.Length {
		return nil, false
	}
	return prepend(pattern.Args, wilds(shape.Length-head.Length)), true
}

func firsts(rows [][]*Pattern) []*Pattern {
	result := make([]*Pattern, len(rows))
	for index, row := range rows {
		result[index] = row[0]
	}
	return result
}
