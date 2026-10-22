package hyrule_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

const deriveHost = `package fixture
type Snapshot struct { Backing int; Cells []int; Guard int; Label string }
type Key struct { Backing int }
type Signature func(Snapshot)(Snapshot,bool)
type FreezeSignature func(Snapshot)Snapshot
type KeySignature func(Snapshot)Key
type EqualSignature func(Snapshot,Snapshot)bool
type VisitSignature func(Snapshot,func(int))
type TransformSignature func(Snapshot,func(int)int)Snapshot
type DisplaySignature func(Snapshot,int)string
// This call requires derivation stubs even before the first generation.
func Capture(v Snapshot) Snapshot { return FreezeSnapshot(v) }
`

const deriveSchema = `type Snapshot = Snapshot
  backing: Backing
  cells: Cells
  guard: Guard
  label: Label

type Key = Key
  backing: Backing

derive Snapshot
  freeze FreezeSnapshot: FreezeSignature
    backing: keep
    cells: clone
    guard: keep
    label: keep
  key SnapshotKey: KeySignature
    backing: keep
    cells: omit
    guard: omit
    label: omit
  equal EqualSnapshot: EqualSignature
    backing: keep
    cells: omit
    guard: omit
    label: omit
  visit VisitSnapshot: VisitSignature
    backing: apply
    cells: each
    guard: keep
    label: keep
  transform TransformSnapshot: TransformSignature
    backing: apply
    cells: each
    guard: keep
    label: keep
  display DisplaySnapshot: DisplaySignature
    backing: keep
    cells: omit
    guard: keep
    label: keep

@Preserve
rewrite value = value
`

func TestDerivedRecordsOwnStoragePreserveEvidenceAndVisitChildren(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", deriveHost)
	write(t, root, "input.rules", deriveSchema)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import("testing";"strings")
func TestDerivedRecords(t *testing.T) {
 original:=Snapshot{Backing:7,Cells:[]int{2,3},Guard:9,Label:"source"}
 frozen:=Capture(original)
 original.Cells[0]=99
 if frozen.Cells[0]!=2 || frozen.Guard!=9 || frozen.Label!="source" { t.Fatal("snapshot lost ownership or evidence") }
 other:=Snapshot{Backing:7,Cells:[]int{8},Guard:1,Label:"other"}
 if !EqualSnapshot(frozen,other) || SnapshotKey(frozen)!=SnapshotKey(other) {t.Fatal("metadata split semantic identity")}
 var children []int
 VisitSnapshot(frozen,func(v int){children=append(children,v)})
 if len(children)!=3 || children[0]!=7 || children[1]!=2 || children[2]!=3 {t.Fatal(children)}
 mapped:=TransformSnapshot(frozen,func(v int)int{return v+10})
 if mapped.Backing!=17 || mapped.Cells[0]!=12 || frozen.Cells[0]!=2 || mapped.Guard!=9 || mapped.Label!="source" {
t.Fatal("transform mutated input or lost evidence")}
 for _, cells:=range [][]int{nil,{}} {
  input:=Snapshot{Cells:cells}
  if (FreezeSnapshot(input).Cells==nil)!=(cells==nil) || (TransformSnapshot(input,func(v int)int{return v}).Cells==nil)!=(cells==nil) {
t.Fatal("nil/empty distinction lost")}
 }
 for _, budget:=range []int{-1,0,1,8,100} {
  text:=DisplaySnapshot(frozen,budget)
  if budget<=0 && text!="" || budget>0 && len(text)>budget {t.Fatalf("budget %d: %q",budget,text)}
 }
 if text:=DisplaySnapshot(frozen,100); !strings.Contains(text,"label=source") {t.Fatal(text)}
}`)
	runUnionFixture(t, root, "TestDerivedRecords")
}

func TestDerivationDiagnosticsAndFormatting(t *testing.T) {
	for _, test := range []struct{ name, replace, with, want string }{
		{"new field needs policy", "    guard: keep\n    label: keep", "    guard: keep", "omits field policy"},
		{"duplicate field", "    backing: keep\n    cells: clone", "    backing: keep\n    backing: keep\n    cells: clone", "duplicate derived field"},
		{"slice ownership", "cells: clone", "cells: keep", "requires borrow, clone"},
		{"unsafe key", "cells: omit", "cells: keep", "safe comparable"},
		{"unknown operation", "freeze FreezeSnapshot", "mystery FreezeSnapshot", "unknown derivation operation"},
		{"wrong callback", "backing: apply", "label: apply\n    backing: keep", "duplicate derived field"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", deriveHost)
			write(t, root, "input.rules", replaceFixture(t, deriveSchema, test.replace, test.with))
			config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = config.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %v, want %q", err, test.want)
			}
		})
	}
	first, err := hyrule.Format("derive.rules", []byte(deriveSchema))
	if err != nil {
		t.Fatal(err)
	}
	second, err := hyrule.Format("derive.rules", first)
	if err != nil || string(first) != string(second) {
		t.Fatalf("non-idempotent format: %v", err)
	}
}

func TestDerivedExactStorageIsOwnedBoundedAndRetainsEvidence(t *testing.T) {
	root := fixture(t)
	host := replaceFixture(t, deriveHost, "package fixture", "package fixture\nimport \"fmt\"") + `
type ID uint8
type ExactKey struct { Backing int; Cells string; Guard int; Label string }
type Table struct { Index map[ExactKey]ID; Values []Snapshot }
type StoreSignature func(Snapshot,*Table,int)(ID,bool)
type LoadSignature func(ID,*Table)(Snapshot,bool)
func cellsKey(values []int) string { return fmt.Sprint(values==nil,values) }
`
	schema := deriveSchema + `type ExactKey = ExactKey
  backing: Backing
  cells: Cells
  guard: Guard
  label: Label

derive Snapshot
  store StoreSnapshot: StoreSignature
    backing: keep
    cells: cellsKey
    guard: keep
    label: keep
  load LoadSnapshot: LoadSignature
`
	write(t, root, "bindings.go", host)
	write(t, root, "input.rules", schema)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestExactStorage(t *testing.T) {
 var table Table
 source:=Snapshot{Backing:3,Cells:[]int{1},Guard:7,Label:"first"}
 first,ok:=StoreSnapshot(source,&table,2)
 if !ok || first==0 {t.Fatal("store failed")}
 source.Cells[0]=99
 retained,ok:=LoadSnapshot(first,&table)
 if !ok || retained.Cells[0]!=1 || retained.Guard!=7 || retained.Label!="first" {t.Fatal("lost ownership/evidence")}
 repeated,ok:=StoreSnapshot(retained,&table,0)
 if !ok || repeated!=first {t.Fatal("existing record consumed budget")}
 retained.Guard=8
 second,ok:=StoreSnapshot(retained,&table,2)
 if !ok || second==first {t.Fatal("exact storage merged read guards")}
 retained.Label="new"
 if _,ok:=StoreSnapshot(retained,&table,2);ok {t.Fatal("entry budget ignored")}
 if _,ok:=LoadSnapshot(0,&table);ok {t.Fatal("zero ID accepted")}
 if _,ok:=LoadSnapshot(99,&table);ok {t.Fatal("foreign ID accepted")}
 if _,ok:=StoreSnapshot(retained,nil,2);ok {t.Fatal("nil table accepted")}
 var overflow Table
 for i:=0;i<255;i++ {
  value:=Snapshot{Backing:i}
  if _,ok:=StoreSnapshot(value,&overflow,1000);!ok {t.Fatal("premature ID exhaustion")}
 }
 if _,ok:=StoreSnapshot(Snapshot{Backing:300},&overflow,1000);ok || len(overflow.Values)!=255 {t.Fatal("ID overflow mutated storage")}
}`)
	runUnionFixture(t, root, "TestExactStorage")
}

// Normalized IDs are issued by the caller's index. Passing them explicitly
// keeps derived key projection pure, without invoking a mutable host interner.
func TestDerivedKeyConsumesTypedNormalizedParameters(t *testing.T) {
	root := fixture(t)
	host := replaceFixture(t, deriveHost, "type KeySignature func(Snapshot)Key", "type KeySignature func(value Snapshot, backingID int)Key")
	schema := replaceFixture(t, deriveSchema, "key SnapshotKey: KeySignature\n    backing: keep", "key SnapshotKey: KeySignature\n    backing: @backingID")
	write(t, root, "bindings.go", host)
	write(t, root, "input.rules", schema)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
import "testing"
func TestNormalizedKey(t *testing.T) {
 if key:=SnapshotKey(Snapshot{Backing:2,Guard:9,Label:"source"},17);key.Backing!=17 {t.Fatal(key)}
}`)
	runUnionFixture(t, root, "TestNormalizedKey")
	for _, bad := range []string{"@missing", "keep"} {
		write(t, root, "input.rules", replaceFixture(t, schema, "@backingID", bad))
		_, err := config.Compile(context.Background(), "")
		if err == nil {
			t.Fatal("accepted missing or unused normalization parameter")
		}
	}
}
