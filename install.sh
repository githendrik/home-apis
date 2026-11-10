#!/bin/sh
# install.sh — install home-apis on a Debian/Ubuntu LXC container
# Usage: curl -fsSL https://github.com/githendrik/home-apis/releases/latest/download/install.sh | sh

set -e

INSTALL_DIR="/opt/home-apis"
SERVICE_NAME="home-apis"
REPO="githendrik/home-apis"
BINARY_NAME="home-apis"

echo "==> Installing ${SERVICE_NAME}..."

# ── Download latest release ────────────────────────────────────────────────
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/')

if [ -z "$LATEST" ]; then
  echo "!! Could not determine latest release" >&2
  exit 1
fi

echo "==> Latest release: ${LATEST}"

INSTALLED_VERSION=""
if [ -f "$INSTALL_DIR/VERSION" ]; then
  INSTALLED_VERSION=$(cat "$INSTALL_DIR/VERSION")
  echo "==> Installed version: ${INSTALLED_VERSION}"
fi

URL="https://github.com/${REPO}/releases/download/${LATEST}/${BINARY_NAME}"

mkdir -p "$INSTALL_DIR"

echo "==> Downloading binary..."
curl -fsSL "$URL" -o "$INSTALL_DIR/$BINARY_NAME.new"
chmod +x "$INSTALL_DIR/$BINARY_NAME.new"
mv "$INSTALL_DIR/$BINARY_NAME.new" "$INSTALL_DIR/$BINARY_NAME"

echo "$LATEST" > "$INSTALL_DIR/VERSION"

echo "==> Installed to $INSTALL_DIR"

# ── Create config if not exists ────────────────────────────────────────────
if [ ! -f "$INSTALL_DIR/config.json" ]; then
  cat > "$INSTALL_DIR/config.json" <<'CONF'
{
  "icloud_username": "your@apple.id",
  "icloud_app_password": "xxxx-xxxx-xxxx-xxxx",
  "calendar_name": "Family",
  "port": 3000,
  "max_events": 3,
  "cache_ttl_seconds": 300,
  "api_token": null,
  "timezone": "Europe/Zurich",
  "kitchen_max_events": 5,
  "solar_source": "fake",
  "solar_poll_seconds": 60
}
CONF
  chmod 600 "$INSTALL_DIR/config.json"
  echo "==> Created config template at $INSTALL_DIR/config.json"
  echo "    Please edit it with your iCloud credentials before starting."
fi

# ── Install systemd service ────────────────────────────────────────────────
cat > /etc/systemd/system/${SERVICE_NAME}.service <<EOF
[Unit]
Description=home-apis (calendar + solar API)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY_NAME} --config ${INSTALL_DIR}/config.json
WorkingDirectory=${INSTALL_DIR}
Restart=on-failure
RestartSec=10

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadOnlyPaths=/
ReadWritePaths=${INSTALL_DIR}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload

if [ -n "$INSTALLED_VERSION" ]; then
  echo "==> Upgrading from ${INSTALLED_VERSION} to ${LATEST}..."
fi

systemctl enable "$SERVICE_NAME"
echo "==> Restarting service..."
systemctl restart "$SERVICE_NAME"

echo ""
echo "==> ${SERVICE_NAME} is running at http://$(hostname -I | awk '{print $1}'):3000"
echo "==> Config:  $INSTALL_DIR/config.json"
echo "==> Manage:  systemctl {start|stop|restart|status} ${SERVICE_NAME}"
echo "==> Logs:    journalctl -u ${SERVICE_NAME} -f"
