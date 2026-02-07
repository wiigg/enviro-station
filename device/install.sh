#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if ! command -v apt-get >/dev/null 2>&1; then
  echo "This installer currently supports Debian/Raspberry Pi OS (apt-get)." >&2
  exit 1
fi

echo "Installing OS dependencies..."
sudo apt-get update
sudo apt-get install -y \
  curl \
  build-essential \
  pkg-config \
  python3 \
  python3-dev \
  python3-pip \
  python3-venv \
  libffi-dev \
  libjpeg-dev \
  zlib1g-dev \
  libopenblas0 \
  libopenblas-dev \
  libportaudio2

if command -v raspi-config >/dev/null 2>&1; then
  echo "Enabling Raspberry Pi interfaces (SPI, I2C, UART)..."
  sudo raspi-config nonint do_spi 0
  sudo raspi-config nonint do_i2c 0
  sudo raspi-config nonint do_serial_cons 1
  sudo raspi-config nonint do_serial_hw 0
fi

if ! command -v uv >/dev/null 2>&1; then
  echo "Installing uv..."
  curl -LsSf https://astral.sh/uv/install.sh | sh
fi

export PATH="$HOME/.local/bin:$PATH"

echo "Creating device virtual environment..."
uv venv --python 3.13 .venv

echo "Syncing Python dependencies..."
uv sync

if [[ ! -f .env.local ]]; then
  cp .env.local.example .env.local
  echo "Created .env.local from template. Fill in BACKEND_BASE_URL and INGEST_API_KEY."
fi

echo "Device bootstrap complete."
echo "Run: source .venv/bin/activate && python main.py"
