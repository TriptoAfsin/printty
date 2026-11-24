#!/bin/sh
# Data in /var/lib/printty (users, settings) is kept, like any service's state.
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
exit 0
