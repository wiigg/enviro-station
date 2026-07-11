#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_SOURCE="$SCRIPT_DIR/config/52envirostation-unattended-upgrades"
CONFIG_TARGET="/etc/apt/apt.conf.d/52envirostation-unattended-upgrades"

if ! command -v apt-get >/dev/null 2>&1; then
  echo "Automatic updates currently support Debian/Raspberry Pi OS (apt-get)." >&2
  exit 1
fi

if [[ ! -f "$CONFIG_SOURCE" ]]; then
  echo "Missing automatic-update policy: $CONFIG_SOURCE" >&2
  exit 1
fi

skip_package_install=false
if [[ $# -eq 1 && "$1" == "--skip-package-install" ]]; then
  skip_package_install=true
elif [[ $# -ne 0 ]]; then
  echo "Usage: $0 [--skip-package-install]" >&2
  exit 1
fi

if [[ "$skip_package_install" == "false" ]]; then
  sudo apt-get update
  sudo apt-get install -y unattended-upgrades
fi

sudo install -o root -g root -m 0644 "$CONFIG_SOURCE" "$CONFIG_TARGET"
sudo systemctl enable --now \
  apt-daily.timer \
  apt-daily-upgrade.timer \
  unattended-upgrades.service

echo "Automatic updates configured."
systemctl list-timers apt-daily.timer apt-daily-upgrade.timer --all --no-pager
