#!/usr/bin/env python3
"""Writes one version of the demo fixture files.

Usage: gen_conflicts.py <base|ours|theirs> <dir>
"""
import os
import sys

SECTIONS = {
    "app.py": 4,
    "big_module.py": 5,
    "CHANGELOG.md": 3,
}


def body(name, n, variant):
    lines = [f"# {name} - demo fixture\n", "\n"]
    for i in range(1, n + 1):
        lines.append(f"# --- section {i} ---\n")
        lines.append(f"value_{i} = {variant}_{i}\n")
        lines.append(f"keep_{i} = same\n")
        if variant == "ours":
            lines.append(f"ours_only_{i} = true\n")
        elif variant == "theirs":
            lines.append(f"theirs_only_{i} = true\n")
        lines.append("\n")
    lines.append("# end\n")
    return "".join(lines)


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in ("base", "ours", "theirs"):
        sys.exit("usage: gen_conflicts.py <base|ours|theirs> <dir>")
    variant, root = sys.argv[1], sys.argv[2]
    os.makedirs(root, exist_ok=True)
    for name, n in SECTIONS.items():
        with open(os.path.join(root, name), "w") as f:
            f.write(body(name, n, variant))


if __name__ == "__main__":
    main()
