#!/usr/bin/env bash
# Picks the image tags a release may set. Reads the tags computed for the
# release (one "<image>:<tag>" per line, as docker/metadata-action prints them)
# on stdin and prints the ones to set, in the same order. The exact version
# (X.Y.Z, X.Y.Z-<pre>) and sha-<7> are always set. A moving tag ("latest", "X",
# "X.Y") is set only when the release is not older than the version the tag
# points at now, read from the image's org.opencontainers.image.version
# annotation in the registry; a tag that does not exist yet is set.
# Prints "release tags: <tag> stays on <version>" on stderr for each moving
# tag left in place. Prints one line "release tags: <reason>" on stderr, nothing
# on stdout, and exits 1 when a tag or a version cannot be read.
#
# Usage: release-tags.sh <git-tag> <image> < tags
set -euo pipefail

fail() {
  echo "release tags: $*" >&2
  exit 1
}

[ "$#" -eq 2 ] || fail "usage: release-tags.sh <git-tag> <image> < tags"
git_tag=$1
image=$2
order="$(dirname "$0")/release-order.sh"
version=${git_tag#v}

tags=()
while IFS= read -r line || [ -n "$line" ]; do
  [ -z "$line" ] && continue
  case "$line" in
    "$image:"*) tags+=("${line#"$image:"}") ;;
    *) fail "$line is not a tag of $image" ;;
  esac
done
[ "${#tags[@]}" -gt 0 ] || fail "no tags"

num='(0|[1-9][0-9]*)'
seen_version=0
for tag in "${tags[@]}"; do
  if [ "$tag" = "$version" ]; then
    seen_version=1
  elif [[ ! $tag =~ ^(latest|sha-[0-9a-f]{7}|${num}|${num}\.${num})$ ]]; then
    fail "unexpected tag $image:$tag"
  fi
done
[ "$seen_version" -eq 1 ] || fail "no tag $image:$version"

# Prints the version the tag points at, or nothing when the tag does not exist.
# Any other registry failure stops the script.
current_version() {
  local ref=$1 err out
  err=$(mktemp)
  # shellcheck disable=SC2016 # a Go template, not a shell expansion
  if out=$(docker buildx imagetools inspect "$ref" \
    --format '{{ index .Manifest.Annotations "org.opencontainers.image.version" }}' 2> "$err"); then
    rm -f "$err"
    [ -n "$out" ] || fail "$ref has no org.opencontainers.image.version annotation"
    printf '%s\n' "$out"
    return 0
  fi
  out=$(cat "$err")
  rm -f "$err"
  # Only "<ref>: not found" means the tag is missing; a missing blob of an
  # existing tag names a digest instead and must stop the script.
  case "$out" in
    *"$ref: not found"*) return 0 ;;
    *) fail "cannot read $ref: $(printf '%s\n' "$out" | head -n 1)" ;;
  esac
}

set_tags=()
for tag in "${tags[@]}"; do
  case "$tag" in
    "$version" | sha-*)
      set_tags+=("$tag")
      continue
      ;;
  esac
  current=$(current_version "$image:$tag")
  take=$("$order" "$git_tag" "$current")
  if [ "$take" = true ]; then
    set_tags+=("$tag")
  else
    echo "release tags: $tag stays on $current" >&2
  fi
done

for tag in "${set_tags[@]}"; do
  printf '%s:%s\n' "$image" "$tag"
done
