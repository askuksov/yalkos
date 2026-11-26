#!/usr/bin/env bash
# Checks that a version tag can be released: the tag is a version, CHANGELOG.md
# has a section for it, and the compose file defaults to the tag's minor version.
# Prints one line "release check: <reason>" on stderr and exits 1 on failure.
#
# Usage: release-check.sh <tag> <changelog> <compose>
set -euo pipefail

fail() {
  echo "release check: $*" >&2
  exit 1
}

[ "$#" -eq 3 ] || fail "usage: release-check.sh <tag> <changelog> <compose>"
tag=$1
changelog=$2
compose=$3

# vX.Y.Z or vX.Y.Z-<pre>, the same shape the workflow trigger matches.
num='(0|[1-9][0-9]*)'
# A numeric pre-release identifier has no leading zero ("rc.01" is not a version).
ident='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
pre="-${ident}(\\.${ident})*"
re="^v(${num})\\.(${num})\\.(${num})(${pre})?\$"
[[ $tag =~ $re ]] || fail "tag $tag is not a version"

major=${BASH_REMATCH[2]}
minor=${BASH_REMATCH[4]}
patch=${BASH_REMATCH[6]}
version="$major.$minor.$patch"
prerelease=0
case "$tag" in *-*) prerelease=1 ;; esac

# Changelog: a dated section for the version; a pre-release may also use
# "Unreleased" (its base version is the version without the suffix).
[ -f "$changelog" ] || fail "CHANGELOG.md not found"
escaped=${version//./\\.}
date_re='[0-9]{4}-[0-9]{2}-[0-9]{2}'
if [ "$prerelease" -eq 1 ]; then
  heading="^## \\[${escaped}\\] - (${date_re}|Unreleased)[[:space:]]*\$"
  missing="CHANGELOG.md has no section for $version"
else
  heading="^## \\[${escaped}\\] - ${date_re}[[:space:]]*\$"
  missing="CHANGELOG.md has no dated section for $version"
fi
grep -Eq "$heading" "$changelog" || fail "$missing"

# Compose: the default image version is the tag's X.Y. Commented lines do not
# count. Only the first image line with a default is looked at.
[ -f "$compose" ] || fail "docker-compose.yml not found"
# shellcheck disable=SC2016 # the dollar sign is literal text in the pattern
default=$(grep -vE '^[[:space:]]*#' "$compose" |
  sed -n 's|.*ghcr\.io/askuksov/yalkos:\${YALKOS_VERSION:-\([^}]*\)}.*|\1|p' |
  sed -n 1p) || true
[ -n "$default" ] || fail "docker-compose.yml has no ghcr.io/askuksov/yalkos image with a YALKOS_VERSION default"
[ "$default" = "$major.$minor" ] || fail "docker-compose.yml defaults to $default, expected $major.$minor"
