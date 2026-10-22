# Applications, names and precedence

Hyrule uses saturated whitespace application. Group nested calls explicitly:

```text
host
  addCall?
  integer?
  GroupAdd

rewrite (addCall receiver (integer delta)) = GroupAdd receiver delta true
```

The outer pattern may omit its parentheses. The nested `integer delta` cannot:
`addCall receiver integer delta` supplies three arguments, without consulting
Go arity to guess grouping. Nullary host calls use `Name()`.

Named groups are argument syntax, not tuples:

```text
Header (backing: backing, length: length, capacity: capacity)
```

Parentheses and brackets allow cosmetic line wrapping. Calls bind tighter than
`* / %`, then `+ -`, comparisons, then `<|>`. Arithmetic is left-associative.
Use `f (-1)` for a negative argument; `f x - 1` subtracts from the call result.
Guard qualifiers use comma or `&&` in written order. Boolean `||` is unsupported.

`whole@Pattern` binds the whole matched value. In patterns, bare names are
captures unless declared as constructors/matchers/reusable patterns. An escaped
name such as `` `instruction` `` forces a capture when that spelling also names
a matcher. `^Limit` is an explicit constant. Result references prefer lexical
captures; declared value aliases refer to constants, and constructors retain
their declared identity. Host functions are not implicitly called as values.

```text
rewrite whole@(Pair left right) = sum (sum left 1) right
```

Fields are explicit: `request.Header.Length`. A record update is
`header with (length: end)`. Parenthesize it when passing it as an argument:
`use (header with (length: end))`.

Pattern `|` requires parentheses, equation `|` introduces guards, and `<|>`
combines committed result strategies. See [layout](LAYOUT.md) for block endings,
[the grammar](grammar.ebnf) for syntax, and [queries](TRANSITIONS.md) for effect
and fallback behavior. The formatter canonicalizes equivalent grouping; it
preserves meaning and comments, not every redundant pair of parentheses.
