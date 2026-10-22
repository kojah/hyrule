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

func TestNamedBuildersPreserveWrittenEvaluationOrder(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Node struct{A,B int}
 type Signature func(Node)(int,bool)
 var Events []int
 func Pair(input Node)(left,right int,matched bool){return input.A,input.B,true}
 func Positive(input int)bool{Events=append(Events,0);return input>0}
 func Mark(value int)int{Events=append(Events,value);return value}
 func Compose(left,right int)int{Events=append(Events,8);return left*10+right}
 func Sum(left,right int)int{Events=append(Events,9);return left+right}
 `)
	write(t, root, "input.rules", `host
  Pair? = Pair
  Positive = Positive
  Mark = Mark
  Compose = Compose
  Sum = Sum

@Guarded
rewrite (Pair (left: x))
  | Positive x
  =
  Compose (right: Mark x, left: Mark 4)

@Fallback
rewrite _ = Sum (Mark 1) (Compose (right: Mark 2, left: Mark 3))
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import("reflect";"testing")
 func TestBuilderOrder(t *testing.T){
  for _,test:=range []struct{input,want int;events []int}{
   {5,45,[]int{0,5,4,8}},
   {-1,33,[]int{0,1,2,3,8,9}},
  }{
   Events=nil;got,ok:=Rewrite(Node{A:test.input})
   if !ok||got!=test.want||!reflect.DeepEqual(Events,test.events){t.Fatalf("%d => %d,%v events=%v",test.input,got,ok,Events)}
  }
 }
 `)
	command := exec.Command("go", "test", "-run", "TestBuilderOrder", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated named-builder behavior: %v\n%s", err, output)
	}
}

func TestNamedBuilderHostAndAssignmentContexts(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Node struct{A,B int}
 type Signature func(Node,*Host)(int,bool)
 type Host struct{}
 func(host *Host) Build(backing *Node,length uint8)int{if backing==nil{return int(length)};return -1}
 `)
	write(t, root, "input.rules", `host
  Header = Build

@Header
rewrite _ = Header (length: 255, backing: nil)
`)
	config, artifacts := compile(t, root)
	if err := config.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestHost(t *testing.T){if got,ok:=Rewrite(Node{},&Host{});!ok||got!=255{t.Fatalf("%d,%v",got,ok)}}`)
	command := exec.Command("go", "test", "-run", "TestHost", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("named host builder: %v\n%s", err, output)
	}
}

func TestNamedBuilderDiagnostics(t *testing.T) {
	for _, test := range []struct{ name, expression, want string }{
		{"missing", `Sum (left: 1)`, "missing named builder arguments"},
		{"duplicate", `Sum (left: 1, left: 2)`, "duplicate builder argument left"},
		{"unknown", `Sum (left: 1, absent: 2)`, "unknown builder argument absent"},
		{"case sensitive", `Sum (Left: 1, right: 2)`, "unknown builder argument Left"},
		{"mixed named first", `Sum (left: 1) 2`, "cannot mix named and positional builder arguments"},
		{"mixed positional first", `Sum 1 (right: 2)`, "cannot mix named and positional builder arguments"},
		{"unnamed Go params", `Unnamed (left: 1, right: 2)`, "usable Go parameter names"},
		{"blank Go param", `Blank (left: 1, right: 2)`, "usable Go parameter names"},
		{"typed literal", `Tiny (value: 256)`, "overflows"},
		{"wrong type", `Sum (left: "wrong", right: 2)`, "cannot use"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", `package fixture
 type Node struct{A,B int}
 type Signature func(Node)(int,bool)
 func Sum(left,right int)int{return left+right}
 func Unnamed(int,int)int{return 0}
 func Blank(_ int,right int)int{return right}
 func Tiny(value uint8)int{return int(value)}
 `)
			write(t, root, "input.rules", `host
  Sum
  Unnamed
  Blank
  Tiny
@Contract
rewrite _ = `+test.expression+"\n")
			config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = config.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestNamedGuardArgumentsRemainUnsupported(t *testing.T) {
	root := fixture(t)
	write(t, root, "input.rules", `host
  Positive = Positive

@Contract
rewrite _
  | Positive (n: 1)
  =
  0
`)
	config, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.Compile(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "not guards") {
		t.Fatalf("named guard unexpectedly accepted: %v", err)
	}
}

func TestNamedBuilderFormatting(t *testing.T) {
	input := []byte(`host
  SliceHeader = Build

@Header
rewrite _ = SliceHeader (backing: b, offset: s, length: n, capacity: c)
`)
	formatted, err := hyrule.Format("builders.rules", input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("builders.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format not idempotent: %v\n%s", err, again)
	}
	if !strings.Contains(string(formatted), "backing: b, offset: s, length: n, capacity: c") {
		t.Fatalf("builder fields lost: %s", formatted)
	}
}
