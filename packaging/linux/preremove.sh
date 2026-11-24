#!/bin/sh
# Stop the service before its files go. On upgrades (deb: "upgrade", rpm: 1)
# the new package restarts it.
if [ -d /run/systemd/system ]; then
  case "$1" in
    upgrade|1) ;;
    *) systemctl disable --now printty >/dev/null 2>&1 || true ;;
  esac
fi
exit 0
