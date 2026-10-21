package hyrule_test

import (
	"fmt"
	"testing"
)

// Integer overflow is a language contract: generated operations wrap using the
// host integer type and do not silently reject the matching rule.
func TestArithmeticLocalsPreserveGoOverflow(t *testing.T) {
	for _, test := range []struct{ typ, input, want string }{
		{"int8", "127", "-128"},
		{"uint8", "255", "0"},
		{"int", "int(^uint(0) >> 1)", "-int(^uint(0) >> 1)-1"},
	} {
		t.Run(test.typ, func(t *testing.T) {
			root := fixture(t)
			write(t, root, "bindings.go", fmt.Sprintf(`package fixture
 type Number struct { Value %s }
 type Signature func(Number)(Number,bool)
`, test.typ))
			write(t, root, "input.rules", `record Number = Number {value: Value;};
rule Increment:
 $number:(Number value:$value)
 let $one = 1
 let $next = (add $value 1)
 => (update $number value:$next);`)
			config, artifacts := compile(t, root)
			if err := config.Publish(artifacts, false); err != nil {
				t.Fatal(err)
			}
			write(t, root, "behavior_test.go", fmt.Sprintf(`package fixture
import "testing"
func TestWrap(t *testing.T) {
 input:=Number{Value:%s}
 got,ok:=Rewrite(input)
 if !ok || got.Value != %s || input.Value != %s {
  t.Fatalf("input=%%+v got=%%+v matched=%%t",input,got,ok)
 }
}`, test.input, test.want, test.input))
			runUnionFixture(t, root, "TestWrap")
		})
	}
}
