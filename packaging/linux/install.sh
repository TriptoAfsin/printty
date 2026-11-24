#!/bin/sh
# Installs Printty as a systemd service. Run from the unpacked download:
#   sudo ./install.sh
# Printty prints through CUPS: install it first (for example: sudo apt install cups).
set -eu
cd "$(dirname "$0")"
if [ "$(id -u)" -ne 0 ]; then
  echo "Run it with sudo: sudo ./install.sh" >&2
  exit 1
fi
install -m 0755 printty /usr/local/bin/printty
install -d -m 0755 /etc/printty
[ -f /etc/printty/printty.env ] || install -m 0600 printty.env.example /etc/printty/printty.env
install -m 0644 printty.service /etc/systemd/system/printty.service
systemctl daemon-reload
systemctl enable --now printty
sleep 2
code=$(cat /var/lib/printty/setup-code.txt 2>/dev/null || true)
echo
echo "Printty is running: open http://$(hostname).local:8631 or http://localhost:8631"
[ -n "$code" ] && echo "Setup code: $code"
echo "Settings (Telegram): /etc/printty/printty.env, then: sudo systemctl restart printty"
echo "Remove it: sudo systemctl disable --now printty && sudo rm /etc/systemd/system/printty.service /usr/local/bin/printty"
