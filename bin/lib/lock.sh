#!/usr/bin/env bash
# lib/lock.sh — atomic mkdir-based lock primitive used by bbs-ticket. First
# module of the bin decomposition (docs/bin-decomposition-spike.md,
# extraction step 1).
#
# Sourced module: function definitions only, no side effects at source time,
# no `set` changes, everything namespaced `bbs_lock_*`. The lock IS the
# directory — `mkdir` is atomic on POSIX, so exactly one caller wins the race.
# Callers own their own lock-path globals, EXIT traps, PID files, and error
# messages; this module is just the acquire-retry loop and the release —
# callers layer their own lock *policies* over this shared *primitive*.

# bbs_lock_acquire <lockdir> [max_tries]
#   Spin on mkdir until it succeeds or max_tries (default 50, ~5s at 0.1s/try)
#   is exceeded. Returns 0 on acquire, 1 on timeout. Writes nothing to stdout.
bbs_lock_acquire() {
  local lockdir="$1" max_tries="${2:-50}" tries=0
  while ! mkdir "$lockdir" 2>/dev/null; do
    tries=$((tries + 1))
    [ "$tries" -gt "$max_tries" ] && return 1
    sleep 0.1
  done
  return 0
}

# bbs_lock_release <lockdir>
#   Remove the lock directory and anything under it (e.g. a PID file).
bbs_lock_release() {
  [ -n "${1:-}" ] && rm -rf "$1" 2>/dev/null || true
}
