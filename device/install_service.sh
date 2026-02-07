#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

SERVICE_USER="${SERVICE_USER:-${SUDO_USER:-$USER}}"
WORKING_DIR="$SCRIPT_DIR"
PROGRAM_PATH="$SCRIPT_DIR/main.py"
SERVICE_PATH="/etc/systemd/system/sensor.service"

if [[ ! -x "$WORKING_DIR/.venv/bin/python" ]]; then
  echo "Missing $WORKING_DIR/.venv/bin/python. Run ./install.sh first." >&2
  exit 1
fi

if [[ ! -f "$PROGRAM_PATH" ]]; then
  echo "Missing $PROGRAM_PATH" >&2
  exit 1
fi

TEMP_SERVICE="$(mktemp)"
cp sensor.service.template "$TEMP_SERVICE"

sed -i "s|<<USER>>|$SERVICE_USER|g" "$TEMP_SERVICE"
sed -i "s|<<WORKING_DIRECTORY>>|$WORKING_DIR|g" "$TEMP_SERVICE"
sed -i "s|<<PATH_TO_DEVICE_PROGRAM>>|$PROGRAM_PATH|g" "$TEMP_SERVICE"

sudo cp "$TEMP_SERVICE" "$SERVICE_PATH"
rm -f "$TEMP_SERVICE"

sudo systemctl daemon-reload
sudo systemctl enable --now sensor.service

echo "Installed and started sensor.service"
sudo systemctl --no-pager --full status sensor.service || true
