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

func TestNamedFieldsAndOrderedAlternatives(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Node struct { Offset, Length int }
 type Signature func(Node) (int,bool)
 var Events []string
 func Header(input Node)(offset,length int,matched bool){return input.Offset,input.Length,true}
 func Integer(input int)(value int,matched bool){return input,true}
 func First(input int)bool{Events=append(Events,"first");return input>0}
 func Second(input int)bool{Events=append(Events,"second");return input>3}
 func Result(offset,length int)int{Events=append(Events,"build");return offset*10+length}
 `)
	write(t, root, "input.rules", `match Header = Header;
 match Integer = Integer;
 guard First = First;
 guard Second = Second;
 build Result = Result;
 // Each OR arm has the same two captures. The inner named capture destructures
 // a certified header component without changing its enclosing correlation.
 rule Choose:
   (or (Header length:(Integer value:$length) offset:$offset)
       (Header offset:$length length:$offset))
   && (First $length)
   && (Second $length)
   => (Result $offset $length);
 rule Fallback: (Header length:$length) => (Result 0 $length);
 `)
	c, artifacts := compile(t, root)
	if err := c.Publish(artifacts, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import("reflect";"testing")
 func TestPatterns(t *testing.T){
  cases:=[]struct{input Node;want int;events []string}{
   {Node{2,5},25,[]string{"first","second","build"}},
   {Node{5,2},25,[]string{"first","second","first","second","build"}},
   {Node{-1,2},2,[]string{"first","second","first","build"}},
  }
  for _,c:=range cases{
   Events=nil;got,ok:=Rewrite(c.input)
   if !ok||got!=c.want||!reflect.DeepEqual(Events,c.events){t.Fatalf("%v: %d %v %v",c.input,got,ok,Events)}
  }
 }
 `)
	command := exec.Command("go", "test", "-run", "TestPatterns", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated named/OR behavior: %v\n%s", err, output)
	}
}

func TestPatternContractDiagnostics(t *testing.T) {
	cases := []struct{ name, pattern, build, want string }{
		{"unknown field", `(Pair missing:$x)`, `0`, "unknown matcher field missing"},
		{"success is not child", `(Pair matched:_)`, `0`, "unknown matcher field matched"},
		{"duplicate field", `(Pair left:$x left:$y)`, `0`, "duplicate matcher field left"},
		{"mixed children", `(Pair left:$x _)`, `0`, "cannot mix named and positional"},
		{"unnamed result", `(Unnamed left:$x)`, `0`, "unknown matcher field left"},
		{"or missing capture", `(or (Pair left:$x) (Pair right:$y))`, `0`, "identical capture names and types"},
		{"or capture type", `(or (Pair left:$x) (Text value:$x))`, `0`, "identical capture names and types"},
		{"local join", `(Pair left:(or $x _) right:$x)`, `0`, "identical capture names and types"},
		{"or arity", `(or _)`, `0`, "at least two alternatives"},
		{"or labels", `(or left:_ _)`, `0`, "cannot have field names"},
		{"or build", `_`, `(or 1 2)`, "only allowed in matcher patterns"},
		{"named build", `_`, `(Sum left:1 2)`, "only allowed in matcher patterns"},
		{"interface equality", `(Any left:$x right:$x)`, `0`, "may contain an interface"},
		{"expansion bound", `(Pair left:(or 1 2 3 4 5 6 7 8 9) right:(or 1 2 3 4 5 6 7 8))`, `0`, "exceeds 64 alternatives"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", `package fixture
 type Node struct{A,B int}
 type Signature func(Node)(int,bool)
 func Pair(input Node)(left,right int,matched bool){return input.A,input.B,true}
 func Unnamed(input Node)(int,int,bool){return input.A,input.B,true}
 func Text(input Node)(value string,matched bool){return "",true}
 func Any(input Node)(left,right any,matched bool){return input.A,input.B,true}
 func Sum(left,right int)int{return left+right}
 `)
			write(t, root, "input.rules", `match Pair = Pair; match Unnamed = Unnamed; match Text = Text; match Any = Any; build Sum = Sum;
 rule Contract: `+test.pattern+` => `+test.build+`;`)
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

func TestNamedAlternativeFormatting(t *testing.T) {
	original := []byte(`// field ordering is intentional
 match Header=Header;
 rule Nested:$whole:(or (Header length:$n offset:(or 0 1)) (Header offset:2 length:$n)) => $n;`)
	formatted, err := hyrule.Format("named.rules", original)
	if err != nil {
		t.Fatal(err)
	}
	again, err := hyrule.Format("named.rules", formatted)
	if err != nil || string(again) != string(formatted) {
		t.Fatalf("format not idempotent: %v\n%s", err, again)
	}
	if !strings.Contains(string(formatted), "length:$n offset:(or 0 1)") {
		t.Fatalf("lost named order: %s", formatted)
	}
}
