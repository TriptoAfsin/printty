#!/bin/sh
# Start Printty as a service and say where to open it.
set -e
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload
  systemctl enable printty >/dev/null 2>&1 || true
  systemctl restart printty || true
  sleep 2
fi
code=$(cat /var/lib/printty/setup-code.txt 2>/dev/null || true)
echo
echo "Printty is installed. Open http://$(uname -n).local:8631 (or http://localhost:8631 on this machine)."
[ -n "$code" ] && echo "Setup code: $code"
echo "Telegram alerts: edit /etc/printty/printty.env, then: sudo systemctl restart printty"
exit 0
