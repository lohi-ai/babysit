#!/usr/bin/env bash
# lib/load-env-file.sh — single canonical dotenv parser for babysit.
#
# Source this file with `. "$PROJECT_ROOT/lib/load-env-file.sh"` to get
# the `_load_env_file` function. Both bbs-env and bbs-secrets share it
# so .env semantics stay identical: shell env wins over file values,
# comments / empty lines / lines with ${...} placeholders are skipped,
# and quoted values have one layer of quotes stripped.

# Parses a .env file and exports variables NOT already in the environment.
# Skips comments, empty lines, and lines with unresolved ${...} placeholders.
# Strips one layer of matching single or double quotes; trims an inline
# `# comment` only on unquoted values; tolerates CRLF line endings.
_load_env_file() {
  local file="$1" line
  [ -f "$file" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ -z "$line" ]] && continue
    [[ "$line" == *'${'* ]] && continue
    if [[ "$line" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*) ]]; then
      local key="${BASH_REMATCH[1]}"
      local val="${BASH_REMATCH[2]}"
      if [[ "$val" =~ ^\"(.*)\"$ ]]; then
        val="${BASH_REMATCH[1]}"
      elif [[ "$val" =~ ^\'(.*)\'$ ]]; then
        val="${BASH_REMATCH[1]}"
      else
        val="${val%% #*}"
      fi
      # Tolerate CRLF — strip a trailing \r left by Windows-style line endings.
      val="${val%$'\r'}"
      # Shell env takes priority over .env values; printenv exit code handles empty-string vars
      if ! printenv "$key" >/dev/null 2>&1; then
        export "$key=$val"
      fi
    fi
  done < "$file"
}
