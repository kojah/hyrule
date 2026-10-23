# Hyrule development

Hyrule owns local matching, decisions and construction. Host traversal, mutable
state, fixed points and applying effects remain consumer responsibilities.
Read README.md and the relevant language contract before changing behavior.
Keep parser, semantic checking and Go emission boundaries intact.

Run `make build` during iteration. Run tests when requested or at an agreed
milestone. On Xenia, use agent-test for heavy commands; Make discovers it.
Race testing belongs in CI. Never commit generated Go or manifests.

Keep production files within 500 physical lines and directories within 20
production files. Split by responsibility; generated Go and tests are exempt.
For implementation work, commit only task changes after validation and push
the current branch to its configured upstream.
