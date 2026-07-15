#!/usr/bin/env bash
# Exercise the offline Chronos CLI operational commands end to end.
# No network or API key required — uses a throwaway database.
set -euo pipefail

cd "$(dirname "$0")/../.."

DB="$(mktemp -d)/cli_ops_demo.db"
export CHRONOS_DB_PATH="$DB"
trap 'rm -f "$DB"' EXIT

run() { echo; echo "\$ chronos $*"; go run ./cli/main.go "$@"; }

run version
run config show
run db init
run db status
run sessions list

echo
echo "✓ Offline CLI operational commands OK."
echo "  Next: 'chronos serve :8420' then 'chronos monitor' in another terminal."
