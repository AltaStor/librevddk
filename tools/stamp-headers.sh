#!/usr/bin/env bash
# Copyright 2026 Andrija Panic and the OpenVDDK contributors
# Copyright 2026 Norvitex OU (AltaStor.com) (modifications)
# SPDX-License-Identifier: Apache-2.0
#
# The copyright and licence lines every source file carries (D-0043, CONTRACT.md §88).
#   tools/stamp-headers.sh [--holder NAME]  adds them where they are missing
#   tools/stamp-headers.sh --check          lists the files without them; fails if any
# A new file gets the copyright line of its author, named with --holder ("Copyright <year> NAME"),
# or by default "Copyright <year> the LibreVDDK contributors".
# A file has the lines when SPDX-License-Identifier appears in its first five lines.
# The comment form follows the file type; scripts keep their "#!" line first.
# include/vixDiskLib.h is generated ("ovxtool compat-header" writes the lines); testdata is left as it is.
set -euo pipefail
cd "$(dirname "$0")/.."

YEAR=$(date +%Y)
COPYRIGHT="Copyright $YEAR the LibreVDDK contributors"
# The identifier in this file's own header is the one in force.
LICENSE_ID=$(head -n 5 "$0" | sed -n 's/.*SPDX-License-Identifier: *//p')

usage() { echo "usage: $0 [--holder NAME | --check]" >&2; exit 2; }
mode=stamp
case "${1:-}" in
  --check) [ $# -eq 1 ] || usage; mode=check ;;
  --holder) [ $# -eq 2 ] && [ -n "$2" ] || usage; COPYRIGHT="Copyright $YEAR $2" ;;
  "") ;;
  *) usage ;;
esac

files() {
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git ls-files
  else
    find . -path ./build -prune -o -path ./conformance-work -prune -o -type f -print | sed 's|^\./||'
  fi | grep -E '\.(go|c|h|sh|py)$|(^|/)Makefile$|(^|/)Containerfile[^/]*$|\.pc\.in$' |
       grep -v -E '(^|/)testdata/|^include/vixDiskLib\.h$' | sort
}

stamped() { head -n 5 "$1" | grep -q 'SPDX-License-Identifier'; }

missing=0
while IFS= read -r f; do
  stamped "$f" && continue
  if [ $mode = check ]; then
    echo "no licence header: $f"; missing=$((missing + 1)); continue
  fi
  tmp=$(mktemp)
  case "$f" in
    *.go)
      printf '// %s\n// SPDX-License-Identifier: %s\n\n' "$COPYRIGHT" "$LICENSE_ID" >"$tmp"
      cat "$f" >>"$tmp" ;;
    *.c|*.h)
      printf '/*\n * %s\n * SPDX-License-Identifier: %s\n */\n\n' "$COPYRIGHT" "$LICENSE_ID" >"$tmp"
      cat "$f" >>"$tmp" ;;
    *)
      if head -n 1 "$f" | grep -q '^#!'; then
        { head -n 1 "$f"; printf '# %s\n# SPDX-License-Identifier: %s\n' "$COPYRIGHT" "$LICENSE_ID"
          tail -n +2 "$f" | { IFS= read -r l && { [ "${l#\#}" != "$l" ] && printf '#\n%s\n' "$l" || printf '\n%s\n' "$l"; }; cat; }
        } >"$tmp"
      else
        { printf '# %s\n# SPDX-License-Identifier: %s\n' "$COPYRIGHT" "$LICENSE_ID"
          { IFS= read -r l && { [ "${l#\#}" != "$l" ] && printf '#\n%s\n' "$l" || printf '\n%s\n' "$l"; }; cat; } <"$f"
        } >"$tmp"
      fi ;;
  esac
  cat "$tmp" >"$f"; rm -f "$tmp"
  echo "stamped: $f"
done < <(files)

if [ $mode = check ] && [ $missing -gt 0 ]; then
  echo "$missing file(s) without the licence header; run tools/stamp-headers.sh" >&2
  exit 1
fi
