#!/usr/bin/env bash
# Install Athanor's git hooks. Idempotent: safe to re-run after pulls.
#
# Run from the repo root: `make hooks` (or `bash scripts/install-hooks.sh`).
# This copies `scripts/hooks/*` into `.git/hooks/`. Git does not track files
# in `.git/hooks/`, so every clone needs this once.

set -e

repo_root="$(git rev-parse --show-toplevel)"
hooks_src="$repo_root/scripts/hooks"
hooks_dst="$repo_root/.git/hooks"

if [ ! -d "$hooks_src" ]; then
	echo "install-hooks: no scripts/hooks directory at $hooks_src" >&2
	exit 1
fi

installed=0
for src in "$hooks_src"/*; do
	[ -f "$src" ] || continue
	name="$(basename "$src")"
	dst="$hooks_dst/$name"
	cp "$src" "$dst"
	chmod +x "$dst"
	installed=$((installed + 1))
	echo "install-hooks: installed $name"
done

if [ "$installed" -eq 0 ]; then
	echo "install-hooks: no hook files found in $hooks_src" >&2
	exit 1
fi

echo "install-hooks: $installed hook(s) installed. Re-run after 'git clone'."
