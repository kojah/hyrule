package hyrule_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kojah/lockdoc/tools/hyrule"
)

func TestDecisionGraphPreservesOrderedContainerChoices(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Header struct{Offset,Length,Capacity int}
 type Node struct{Op int;Header,Other *Header}
 type Signature func(Node,*Host)(int,bool)
 type Host struct{Limit int}
 var Calls,Builds int // Test instrumentation only; never affects hook results.
 func Slice(n Node)(header *Header,ok bool){Calls++;return n.Header,n.Op==1}
 func Other(n Node)(header *Header,ok bool){Calls++;return n.Other,n.Op==1}
 func Decode(h *Header)(offset,length,capacity int,ok bool){Calls++;if h==nil{return 0,0,0,false};return h.Offset,h.Length,h.Capacity,true}
 func(h *Host) Fits(length int)bool{return length<=h.Limit}
 func Result(value int)int{Builds++;return value}
 `)
	write(t, root, "input.rules", `host
  Slice? = Slice
  Header? = Decode
  Other? = Other
  Fits = Fits
  Result = Result

@Empty
rewrite (Slice (header: Header (length: 0))) = Result 0

@Reuse
rewrite (Slice (header: Header (length: length)))
  | Fits length
  =
  Result 1

@Grow
rewrite (Slice (header: Header (length: _))) = Result 2
`)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestDecisions(t *testing.T){
 for op:=0;op<3;op++{for length:=-1;length<5;length++{for limit:=-1;limit<4;limit++{
  Calls,Builds=0,0
  got,ok:=Rewrite(Node{Op:op,Header:&Header{Length:length}},&Host{Limit:limit})
  want:=2;if length==0{want=0}else if length<=limit{want=1}
  if ok!=(op==1)||ok&&got!=want{t.Fatalf("%d/%d/%d => %d,%v want %d",op,length,limit,got,ok,want)}
  if op==1&&Calls>2||op!=1&&Calls>1{t.Fatalf("repeated extraction: %d",Calls)}
  if ok&&Builds!=1||!ok&&Builds!=0{t.Fatalf("losing builder ran: %d",Builds)}
 }}}
 Calls,Builds=0,0
 if _,ok:=Rewrite(Node{Op:1},&Host{});ok||Calls>2||Builds!=0{t.Fatal("failed nested matcher did not fall through safely")}
 }
 `)
	runDecisionBehavior(t, root)
	// A failed guard on the first OR alternative must try the second, using its
	// own extracted value. The fallback must not reuse a different input's field.
	write(t, root, "input.rules", `host
  Slice? = Slice
  Header? = Decode
  Other? = Other
  Fits = Fits
  Result = Result

@Alternative
rewrite (Slice (header: Header (length: length)) | Other (header: Header (length: length)))
  | Fits length
  =
  Result length

@Fallback
rewrite _ = Result (-1)
`)
	c, a = compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestDecisions(t *testing.T){
  for left:=-1;left<4;left++{for right:=-1;right<4;right++{
   n:=Node{Op:1,Header:&Header{Length:left},Other:&Header{Length:right}}
   for limit:=-1;limit<4;limit++{
    Builds=0;got,ok:=Rewrite(n,&Host{Limit:limit});want:=-1
    if left<=limit{want=left}else if right<=limit{want=right}
    if !ok||got!=want||Builds!=1{t.Fatalf("%d/%d limit %d => %d,%v builds %d want %d",left,right,limit,got,ok,Builds,want)}
   }
  }}
 }
 `)
	runDecisionBehavior(t, root)
}

func runDecisionBehavior(t *testing.T, root string) {
	t.Helper()
	command := exec.Command("go", "test", "-run", "TestDecisions", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated decision behavior: %v\n%s", err, output)
	}
}

func TestDecisionGraphChecksUnreachableRules(t *testing.T) {
	root := fixture(t)
	write(t, root, "input.rules", `host
  Sum = Sum

@First
rewrite _ = Sum 1 2

@Invalid
rewrite _ = Sum "wrong" 2
`)
	config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.Compile(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "cannot use") {
		t.Fatalf("unreachable builder escaped type validation: %v", err)
	}
}

func TestDecisionBenchmarkGenerationFresh(t *testing.T) {
	config, err := hyrule.Load("testdata/decisionbench/hyrule.yaml")
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := config.Compile(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err = config.Publish(artifacts, true); err != nil {
		t.Fatal(err)
	}
}
