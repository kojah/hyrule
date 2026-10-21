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

const nativeHost = `package fixture
import goast "go/ast"
import "strings"
var _ = strings.HasPrefix
var _ = goast.Ident{}
type Signature func(any, int, *Host) (int, int)
type Host struct { Calls []string; Offset struct { Value int } }
type Kind int
const (Yes Kind = iota; No; Unknown)
func Ready(h *Host, name string) bool { h.Calls=append(h.Calls,"ready"); return name!="skip" }
func Ask(h *Host,n,offset int)(Kind,int) { h.Calls=append(h.Calls,"ask");return Kind(n),offset+7 }
`
const nativeRules = `signature total fallback outputs 0 -1;
view Entry = *goast.Ident { name:Name; };
query ready($name) = Ready $host $name;
query ask($n, $context) = Ask $context $n $context.Offset.Value;
guard named($name) = strings.HasPrefix $name "";
union Proof = (Kind, int) tag 0 {
 Proven = Yes { reason:1; };
 Disproven = No;
 Unavailable = Unknown { reason:1; };
};
rule Main:
 inputs (Entry name:$name) $n $host
 let $offset = $host.Offset.Value
 && $offset >= 0
 && named $name
 => query ready $name {
  false => outputs 10 20;
  true => query ask $n $host {
   Proven reason:$why => outputs $why 1;
   Disproven => outputs 0 2;
   Unavailable reason:$why => outputs $why 3;
  };
 };
`

func TestNativeHostInterfaces(t *testing.T) {
	for _, mode := range []string{"total", "partial"} {
		t.Run(mode, func(t *testing.T) {
			root := fixture(t)
			host, rules := nativeHost, nativeRules
			if mode == "partial" {
				host = strings.Replace(host, "(int, int)", "(int, int, bool)", 1)
				rules = strings.Replace(rules, "signature total fallback outputs 0 -1;", "signature partial;", 1)
			}
			formatted, err := hyrule.Format("input.rules", []byte(rules))
			if err != nil {
				t.Fatal(err)
			}
			again, err := hyrule.Format("input.rules", formatted)
			if err != nil || string(again) != string(formatted) {
				t.Fatalf("format roundtrip: %v", err)
			}
			write(t, root, "bindings.go", host)
			write(t, root, "input.rules", string(formatted))
			c, a := compile(t, root)
			if err := c.Publish(a, false); err != nil {
				t.Fatal(err)
			}
			invocation := "a,b:=Rewrite(tt.input,tt.n,h);ok:=tt.ok"
			failure := "-1"
			if mode == "partial" {
				invocation = "a,b,ok:=Rewrite(tt.input,tt.n,h)"
				failure = "0"
			}
			write(t, root, "behavior_test.go", `package fixture
import("go/ast";"reflect";"testing")
func TestBehavior(t *testing.T){
 for _,tt:=range []struct{input any;n,a,b int;calls []string;ok bool}{
 {&ast.Ident{Name:"ok"},0,12,1,[]string{"ready","ask"},true},
 {&ast.Ident{Name:"ok"},1,0,2,[]string{"ready","ask"},true},
 {&ast.Ident{Name:"ok"},2,12,3,[]string{"ready","ask"},true},
 {&ast.Ident{Name:"skip"},0,10,20,[]string{"ready"},true},
 {&ast.Ident{Name:"ok"},99,0,`+failure+`,[]string{"ready","ask"},false},
 {(*ast.Ident)(nil),0,0,`+failure+`,nil,false},
 {nil,0,0,`+failure+`,nil,false},
 {42,0,0,`+failure+`,nil,false},
 }{
 h:=&Host{};h.Offset.Value=5;`+invocation+`
 if a!=tt.a||b!=tt.b||ok!=tt.ok||!reflect.DeepEqual(h.Calls,tt.calls){t.Fatalf("%#v: %d,%d,%v calls %v",tt.input,a,b,ok,h.Calls)}
 }
}`)
			runGeneratedTransitionTest(t, root)
			cmd := exec.Command("go", "vet", ".")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated native API vet: %v\n%s", err, output)
			}
		})
	}
}

func TestNativeInterfaceDiagnostics(t *testing.T) {
	for _, tt := range []struct{ name, rules, want string }{
		{"bool missing", strings.Replace(nativeRules, "false => outputs 10 20;", "", 1), "non-exhaustive Boolean"},
		{"bool duplicate", strings.Replace(nativeRules, "false => outputs 10 20;", "false => outputs 10 20; false => outputs 0 0;", 1), "duplicate Boolean"},
		{"tuple missing", strings.Replace(nativeRules, "Disproven => outputs 0 2;", "", 1), "non-exhaustive query"},
		{"tuple slot", strings.Replace(nativeRules, "reason:1;", "reason:9;", 1), "missing or invalid direct variant"},
		{"projection", strings.Replace(nativeRules, "Offset.Value", "Missing.Value", 1), "missing or inaccessible field"},
		{"view field", strings.Replace(nativeRules, "name:Name;", "name:Missing;", 1), "missing or inaccessible direct record field"},
		{"view construction", strings.Replace(nativeRules, "outputs 10 20", "Entry name:1", 1), "read-only"},
		{"nested forwarding", strings.Replace(nativeRules, "Ready $host $name", "Ready $host (Ready $host $name)", 1), "forwarding arguments"},
		{"typed forwarding", strings.Replace(nativeRules, "Ready $host $name", "Ready $host 99", 1), "cannot use"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", nativeHost)
			write(t, root, "input.rules", tt.rules)
			c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Compile(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q: %v", tt.want, err)
			}
		})
	}
}

func TestNativeParametersAreNotQueryOutcomes(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Kind int
 const (Yes Kind=iota;No)
 type Signature func(Kind,int) int
 `)
	write(t, root, "input.rules", `signature total fallback outputs -1;
 union Proof=(Kind,int) tag 0 { Proven=Yes; Disproven=No; };
 rule Any: inputs _ _ => outputs 42;
 `)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBehavior(t *testing.T){if got:=Rewrite(Kind(99),7);got!=42{t.Fatalf("ordinary argument constrained by unused outcome schema: %d",got)}}`)
	runGeneratedTransitionTest(t, root)
}

func TestNativeViewRejectsRecursivePointerStorage(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", nativeHost+"\ntype Recursive *Recursive\n")
	write(t, root, "input.rules", strings.Replace(nativeRules, "*goast.Ident", "Recursive", 1))
	c, err := hyrule.Load(filepath.Join(root, "hyrule.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Compile(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "record storage must be a Go struct") {
		t.Fatalf("recursive pointer storage: %v", err)
	}
}

func TestNativeBooleanResultIsData(t *testing.T) {
	root := fixture(t)
	write(t, root, "bindings.go", `package fixture
 type Host struct{ Calls int }
 type Signature func(bool,*Host) bool
 func Observe(host *Host,value bool)bool{host.Calls++;return !value}
 `)
	write(t, root, "input.rules", `signature total fallback outputs false;
 query observe($value)=Observe $host $value;
 rule Invert: inputs $input $host => query $answer=observe $input decide $answer {
  true=>outputs $answer;
  false=>outputs $answer;
 };
 `)
	c, a := compile(t, root)
	if err := c.Publish(a, false); err != nil {
		t.Fatal(err)
	}
	write(t, root, "behavior_test.go", `package fixture
 import "testing"
 func TestBehavior(t *testing.T){h:=&Host{};if !Rewrite(false,h)||Rewrite(true,h)||h.Calls!=2{t.Fatalf("Boolean output confused with success flag: calls=%d",h.Calls)}}`)
	runGeneratedTransitionTest(t, root)
}
