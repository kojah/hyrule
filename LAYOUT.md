# Hyrule source layout

The handwritten frontend implements [grammar.ebnf](grammar.ebnf). The grammar
uses EBNF: brackets are optional, braces mean repetition, quoted text is literal,
and a vertical bar separates alternatives. Semantic validation follows parsing;
constructor identity and host types are not lexical properties.

Top-level declarations start in column one. Tabs in indentation are rejected.
Blank lines and comments do not open or close blocks. `//` and `/* ... */`
comments are preserved by formatting, including comments inside declarations.
The formatter moves internal comments before their containing declaration.

`fn` bodies, `match` arms, `host`, schema bodies, `do`, `case ... of`, and multiline
`else` introduce blocks. The first child establishes the block's indentation;
subsequent siblings align with it. A smaller indentation closes the block.
A greater indentation continues a statement or begins an explicitly introduced
child block. There are no braces or semicolons as alternative Hyrule block delimiters.

`require expression` is a standalone step aligned with other bindings inside
a `do` block; it does not introduce a block or take an `else` clause.

Equations may continue on indented lines. Guard bars belong to the equation;
comma-separated guard qualifiers preserve written evaluation order. A `where`
block follows the last alternative on an indented line; its bindings align with
each other. In `if`, `then` and `else` may each start a line indented past the
line containing `if`. Parentheses, record braces
and brackets permit expression continuation across lines, so named groups and
lists can wrap freely. Explicit `do` and `case` blocks inside parentheses still
use their own statement/arm indentation. Pattern alternatives require parentheses. Result choice `<|>` can
start a continuation line aligned with the first strategy, including `do`.
It combines whole strategies after their inner blocks have ended.

The parser must retain original filename, line and column for diagnostics. Files
are limited to 100,000 tokens and expressions to 64 levels of nesting. Missing
operands, inconsistent sibling indentation, unclosed delimiters and trailing
unrecognized text are errors; no rule is silently discarded.

Backticks quote identifiers, not strings. Use double-quoted string literals for
values. `^Name` explicitly references a constant. Ordinary names remain unresolved
until the frontend knows the declarations and captures of the target. An explicit
empty argument list distinguishes a nullary host call, `Name()`, from a value.

## Embedded Go

`go! { ... }` is one expression token. Braces inside Go strings, rune literals
and comments do not close it. Nested braces are balanced. Go newlines and tabs
inside the braces do not affect Hyrule indentation. The formatter preserves the
body verbatim, including comments and raw string contents; generated Go is
formatted normally. Hyrule resumes immediately after the closing brace.
Embedded tokens count toward the file token limit.


Function headers use `fn name(parameter: Type) -> Result`; `partial` and
`total` precede `fn`. A total function puts `fallback expression` before its
body. A `match parameter` body has aligned arms, each with an optional
`@Label`, a pattern, optional `when` qualifiers, and `-> result`. The body
may instead be a direct result expression. A header without a body can supply
types for equation fragments in the target's other files.

Named record patterns and constructions use `Type { field, other: value }`;
updates use `value with { field: replacement }`. A bare field is a pun for the
same-named value or capture. `pattern as name` binds the whole pattern atom;
group positional applications before aliasing. Record braces are expression
delimiters, not block delimiters. The old named parentheses and prefix `@`
captures are rejected; `@Label` still names a rule.
