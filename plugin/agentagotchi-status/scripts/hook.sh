#!/bin/sh
set -eu

bridge="${AGENTAGOTCHI_BRIDGE:-}"
if [ -z "$bridge" ]; then
  bridge="$HOME/Library/Application Support/Agentagotchi/bin/agentagotchi"
fi
if [ ! -x "$bridge" ]; then
  bridge="$(command -v agentagotchi 2>/dev/null || true)"
fi

if [ -n "$bridge" ] && [ -x "$bridge" ]; then
  exec "$bridge" hook
fi

# Consume the hook payload without storing or echoing it. Stop and
# SubagentStop require valid JSON on stdout, so return an inert object.
while IFS= read -r _line; do :; done
printf '{}\n'
