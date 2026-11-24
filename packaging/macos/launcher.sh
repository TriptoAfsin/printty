#!/bin/sh
# Printty.app: starts Printty for this user (if it isn't running) and opens
# it in the browser. Printty keeps running while the app is open; quit the
# app from the Dock to stop it. Data: ~/Library/Application Support/Printty.
# The program is printty-server: macOS file names ignore case, so it can't
# be "printty" next to this "Printty" launcher.
DIR="$(cd "$(dirname "$0")" && pwd)"
DATA="$HOME/Library/Application Support/Printty"
mkdir -p "$DATA"
if nc -z localhost 8631 >/dev/null 2>&1; then
  open "http://localhost:8631/admin"
  exit 0
fi
( sleep 2; open "http://localhost:8631/admin" ) &
exec "$DIR/printty-server" -data "$DATA" -background
