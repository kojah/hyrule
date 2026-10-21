// Package derived demonstrates schema-owned representation maintenance.
// Exact storage is caller-owned, bounded by entry count, and not concurrent.
package derived

import (
	"strconv"
	"strings"
)

// Snapshot retains an immutable copied payload and its transient read evidence.
type Snapshot struct {
	Backing int
	Cells   []int
	Guard   int
	Label   string
}

// SemanticKey excludes read evidence and diagnostic spelling.
type SemanticKey struct {
	Backing int
	Cells   string
}

// ExactKey retains all evidence. encodeCells preserves nil versus empty storage.
type ExactKey struct {
	Backing int
	Cells   string
	Guard   int
	Label   string
}

// ID is table-local; zero is absent.
type ID uint32

// Table is exclusively owned by its caller. Published Values and Index are
// immutable to consumers; only generated StoreSnapshot mutates this storage.
type Table struct {
	Index  map[ExactKey]ID
	Values []Snapshot
}

type (
	FreezeSignature    func(Snapshot) Snapshot
	KeySignature       func(Snapshot) SemanticKey
	EqualSignature     func(Snapshot, Snapshot) bool
	VisitSignature     func(Snapshot, func(int) bool) bool
	ObserveSignature   func(Snapshot, func(int))
	TransformSignature func(Snapshot, func(int) int) Snapshot
	DisplaySignature   func(Snapshot, int) string
	StoreSignature     func(Snapshot, *Table, int) (ID, bool)
	LoadSignature      func(ID, *Table) (Snapshot, bool)
)

// Capture exercises generated APIs on first generation, before outputs exist.
func Capture(input Snapshot) Snapshot { return FreezeSnapshot(input) }

// encodeCells is an exact pure leaf policy, not a digest or mutable interner.
// Real analysis values should use their existing typed identity boundary.
func encodeCells(cells []int) string {
	if cells == nil {
		return "nil"
	}
	var out strings.Builder
	out.WriteByte('[')
	for _, cell := range cells {
		out.WriteString(strconv.Itoa(cell))
		out.WriteByte(';')
	}
	out.WriteByte(']')
	return out.String()
}
