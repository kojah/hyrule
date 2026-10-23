#!/usr/bin/env python3
"""Enforce Hyrule source budgets independently of consumer repositories."""
from pathlib import Path
import re
import subprocess
import sys

root = Path(__file__).resolve().parent.parent
paths = subprocess.check_output(
    ["git", "ls-files", "--cached", "--others", "--exclude-standard", "*.go"],
    cwd=root, text=True,
).splitlines()
counts = {}
errors = []
for name in sorted(set(paths)):
    path = root / name
    if name.endswith("_test.go") or not path.is_file():
        continue
    source = path.read_text()
    if re.search(r"^// Code generated .* DO NOT EDIT\.$", source, re.MULTILINE):
        continue
    counts[path.parent] = counts.get(path.parent, 0) + 1
    lines = len(source.splitlines())
    if lines > 500:
        errors.append(f"{name}: {lines} production lines (limit 500)")
for directory, count in sorted(counts.items()):
    if count > 20:
        errors.append(f"{directory.relative_to(root)}: {count} production files (limit 20)")
if errors:
    print("\n".join(errors), file=sys.stderr)
    sys.exit(1)
print("Hyrule source budgets passed")
