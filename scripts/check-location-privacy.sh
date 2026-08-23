#!/usr/bin/env bash

set -euo pipefail

# Keep matches out of CI output: only a revision identifier is reported.
location_assignment="(?i)(?<![A-Z0-9_])['\"]?OUTDOOR_LOCATION['\"]?[ \t]*(?:=|:)[ \t]*(?:\"[^\"\r\n]*[^ \t\"\r\n][^\"\r\n]*\"|'[^'\r\n]*[^ \t'\r\n][^'\r\n]*'|[^# \t\r\n])"
uk_postcode="(?i)(?<![#A-Z0-9])(?:GIR[ ]?0AA|(?:[A-PR-UWYZ][0-9][0-9A-HJKSTUW]?|[A-PR-UWYZ][A-HK-Y][0-9][0-9ABEHMNPRV-Y]?)[ ]?[0-9][ABD-HJLNP-UW-Z]{2})(?![A-Z0-9])"

match_file="$(mktemp)"
trap 'rm -f "$match_file"' EXIT

is_generated_file() {
  case "${1##*/}" in
    *.[sS][vV][gG]|*.[sS][vV][gG][zZ]|*.lock|*-lock.json|*lock.yaml|*lock.yml|bun.lockb|go.sum|*.sum|*.sha1|*.sha224|*.sha256|*.sha384|*.sha512|[cC][hH][eE][cC][kK][sS][uU][mM][sS].*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

forbidden_path_kind() {
  case "$1" in
    outputs/*|*/outputs/*)
      printf '%s' "outputs directory"
      ;;
    .env|*/.env|.envrc|*/.envrc|.env.*|*/.env.*)
      case "$1" in
        .env.example|*/.env.example|.env.*.example|*/.env.*.example)
          return 1
          ;;
      esac
      printf '%s' "runtime environment file"
      ;;
    fly.toml|*/fly.toml)
      printf '%s' "runtime Fly configuration"
      ;;
    *)
      return 1
      ;;
  esac
}

stream_contains_location() {
  perl -0777 -e '
    my ($assignment, $postcode) = @ARGV;
    my $text = do { local $/; <STDIN> };
    exit(($text =~ /$assignment/ || $text =~ /$postcode/) ? 0 : 1);
  ' "$location_assignment" "$uk_postcode"
}

check_paths() {
  local kind

  : >"$match_file"
  git ls-files --cached --others --exclude-standard >"$match_file"

  if stream_contains_location <"$match_file"; then
    printf '%s\n' "privacy check failed: prohibited location content found in a working-tree path" >&2
    return 1
  fi

  while IFS= read -r path; do
    if kind="$(forbidden_path_kind "$path")"; then
      printf 'privacy check failed: %s found in the working tree\n' "$kind" >&2
      return 1
    fi
  done <"$match_file"
}

check_revision_paths() {
  local revision="$1"
  local short_revision="${revision:0:12}"

  if git ls-tree -r --name-only "$revision" | stream_contains_location; then
    printf 'privacy check failed: prohibited location content found in a path at revision %s\n' "$short_revision" >&2
    return 1
  fi
}

check_content() {
  local label="$1"
  local revision="${2:-}"
  local status=0
  local path

  : >"$match_file"
  if [[ -n "$revision" ]]; then
    git grep -I -l -P -e "$location_assignment" -e "$uk_postcode" "$revision" -- . >"$match_file" || status=$?
  else
    git grep -I -l -P -e "$location_assignment" -e "$uk_postcode" -- . >"$match_file" || status=$?
  fi
  if [[ "$status" -gt 1 ]]; then
    return "$status"
  fi

  while IFS= read -r match; do
    if [[ -n "$revision" ]]; then
      path="${match#*:}"
    else
      path="$match"
    fi
    if ! is_generated_file "$path"; then
      printf 'privacy check failed: prohibited location content found in %s\n' "$label" >&2
      return 1
    fi
  done <"$match_file"

  if [[ -z "$revision" ]]; then
    while IFS= read -r -d '' path; do
      if ! is_generated_file "$path" && stream_contains_location <"$path"; then
        printf 'privacy check failed: prohibited location content found in %s\n' "$label" >&2
        return 1
      fi
    done < <(git ls-files --others --exclude-standard -z)
  fi
}

check_commit_message() {
  local revision="$1"
  local short_revision="${revision:0:12}"

  if git show -s --format=%B "$revision" | stream_contains_location; then
    printf 'privacy check failed: prohibited location content found in commit message %s\n' "$short_revision" >&2
    return 1
  fi
}

check_paths
check_content "the working tree"
if git for-each-ref --format='%(refname)' | stream_contains_location; then
  printf '%s\n' "privacy check failed: prohibited location content found in a Git ref" >&2
  exit 1
fi

while IFS= read -r revision; do
  [[ -n "$revision" ]] || continue
  short_revision="${revision:0:12}"
  check_content "revision $short_revision" "$revision"
  check_revision_paths "$revision"
  check_commit_message "$revision"
done < <(git rev-list --all --reflog | sort -u)

printf '%s\n' "Location privacy check passed."
