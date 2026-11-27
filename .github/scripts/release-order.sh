#!/usr/bin/env bash
# Decides whether a release may take a moving tag (an image tag such as
# "latest" or "X.Y", or GitHub's "Latest" release) from the version that holds
# it now. Prints "true" when <version> is not older than <current>, or when
# nothing holds the tag yet (<current> empty or left out), and "false" when
# <version> is older. Versions compare by semantic version precedence.
# Prints one line "release order: <reason>" on stderr and exits 1 when either
# version cannot be read.
#
# Usage: release-order.sh <version> [<current>]
#   <version>  the release, vX.Y.Z or X.Y.Z (a pre-release never takes a moving tag)
#   <current>  the version that holds the tag, vX.Y.Z[-<pre>] or X.Y.Z[-<pre>]
set -euo pipefail

fail() {
  echo "release order: $*" >&2
  exit 1
}

[ "$#" -eq 1 ] || [ "$#" -eq 2 ] || fail "usage: release-order.sh <version> [<current>]"
version=$1
current=${2:-}

num='(0|[1-9][0-9]*)'
# A numeric pre-release identifier has no leading zero ("rc.01" is not a version).
ident='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
pre="-${ident}(\\.${ident})*"
re="^v?(${num})\\.(${num})\\.(${num})(${pre})?\$"

[[ $version =~ $re ]] || fail "$version is not a version"
[ -z "${BASH_REMATCH[7]}" ] || fail "$version is a pre-release"
new=("${BASH_REMATCH[2]}" "${BASH_REMATCH[4]}" "${BASH_REMATCH[6]}")

if [ -z "$current" ]; then
  echo true
  exit 0
fi
[[ $current =~ $re ]] || fail "the tag holds $current, which is not a version"
old=("${BASH_REMATCH[2]}" "${BASH_REMATCH[4]}" "${BASH_REMATCH[6]}")

# Major, minor, patch decide. With the same three numbers the release wins:
# it is either the same version or the final version of a pre-release.
for i in 0 1 2; do
  if [ "${new[i]}" -gt "${old[i]}" ]; then
    echo true
    exit 0
  fi
  if [ "${new[i]}" -lt "${old[i]}" ]; then
    echo false
    exit 0
  fi
done
echo true
