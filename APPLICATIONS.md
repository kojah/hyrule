# Optional application parentheses

Parentheses are optional around an outer application where surrounding syntax
already supplies its boundary. Both spellings remain valid, and formatting
preserves the choice. Whitespace and indentation do not determine grouping.

```text
match addCall, integer;

rule KnownCount:
  addCall $receiver (integer $delta)
  => GroupAdd $receiver $delta true;
```

The same rule can retain its parentheses:

```text
rule KnownCount:
  (addCall $receiver (integer $delta))
  => (GroupAdd $receiver $delta true);
```

Optional outer parentheses apply to rule patterns and results, reusable-pattern
bodies, guard and local-binding expressions, query calls, and decision-arm
patterns and results. Queries still execute once when reached and decisions
still handle every outcome:

```text
query commandUse;

rule Return:
  $request
  => query $use = commandUse $request
     decide $use {
       Uncertain => Decision state:Unknown reason:Cutoff;
       Unused => Decision state:Unknown reason:Detached;
       Used => Decision state:Proven reason:Unowned;
     };
```

A bare name at one of these boundaries is resolved against declarations from
all files in the target. A declared constant remains a constant; a declared
constructor, matcher, builder, guard, query or reusable pattern is a nullary
application. Missing arguments remain an error. No grouping is inferred from
Go function arity.

Nested calls still require parentheses. This includes calls inside another
call, list/effect elements and captured subpatterns:

```text
rule Nested:
  $whole:(Pair $left $right)
  => Sum (Sum $left 1) $right;
```

`Sum Sum $left 1 $right` does not mean the same thing: the second `Sum` is an
argument, not an automatically grouped call. Keywords delimiting outer
expressions (`when`, `let`, and `decide`) also end an unparenthesized application;
use the parenthesized form when such a name must be an argument. Existing
parenthesized expressions keep their original grammar and interpretation.
