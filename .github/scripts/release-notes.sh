#!/usr/bin/env bash
# Prints the release notes for a version tag: the body of its CHANGELOG.md
# section (heading excluded). A pre-release gets the first line
# "Release candidate for X.Y.Z." and a blank line before the body.
# Exits 1 with one line on stderr when the section is missing or empty.
#
# Usage: release-notes.sh <tag> <changelog>
set -euo pipefail

fail() {
  echo "release notes: $*" >&2
  exit 1
}

[ "$#" -eq 2 ] || fail "usage: release-notes.sh <tag> <changelog>"
tag=$1
changelog=$2

num='(0|[1-9][0-9]*)'
pre='-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*'
re="^v(${num})\\.(${num})\\.(${num})(${pre})?\$"
[[ $tag =~ $re ]] || fail "tag $tag is not a version"
version="${BASH_REMATCH[2]}.${BASH_REMATCH[4]}.${BASH_REMATCH[6]}"
prerelease=0
case "$tag" in *-*) prerelease=1 ;; esac

[ -f "$changelog" ] || fail "CHANGELOG.md not found"

# The first "## [X.Y.Z] - <date>" heading (or "Unreleased" for a pre-release)
# starts the section; the next "## [" heading or a version link reference
# definition ("[X.Y.Z]: https://...", "[Unreleased]: https://...") ends it, so a
# changelog footer stays out of the notes while "[docs]: https://..." inside a
# body stays in. Blank lines around the body are dropped.
# Exit status of awk: 3 = no such section, 4 = section without a body.
status=0
body=$(awk -v head="## [$version] - " -v allow_unreleased="$prerelease" '
  state == 0 {
    if (index($0, head) == 1) {
      rest = substr($0, length(head) + 1)
      if (rest ~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][ \t\r]*$/ ||
          (allow_unreleased == 1 && rest ~ /^Unreleased[ \t\r]*$/)) {
        state = 1
      }
    }
    next
  }
  /^## \[/ || /^\[(Unreleased|[0-9]+\.[0-9]+\.[0-9]+[^]]*)\]:[ \t]/ { exit }
  /^[ \t\r]*$/ { if (started) pending++; next }
  {
    for (; pending > 0; pending--) print ""
    started = 1
    print
  }
  END {
    if (state == 0) exit 3
    if (!started) exit 4
  }
' "$changelog") || status=$?

case "$status" in
  0) ;;
  3) fail "CHANGELOG.md has no section for $version" ;;
  4) fail "CHANGELOG.md section for $version is empty" ;;
  *) fail "cannot read CHANGELOG.md" ;;
esac

if [ "$prerelease" -eq 1 ]; then
  printf 'Release candidate for %s.\n\n' "$version"
fi
printf '%s\n' "$body"
