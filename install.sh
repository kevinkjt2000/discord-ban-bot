#!/usr/bin/env bash
set -euo pipefail

APP_USER="ban-bot"
APP_DIR="/opt/ban-bot"
ENV_FILE="${APP_DIR}/env"
SYSTEMD_SERVICE="/etc/systemd/system/ban-bot.service"
BINARY="ban-bot"

# Step 1: create system user
if ! id -u "${APP_USER}" >/dev/null 2>&1; then
	echo "Creating system user: ${APP_USER}"
	useradd --system --no-create-home --shell /usr/sbin/nologin "${APP_USER}"
else
	echo "System user ${APP_USER} already exists"
fi

# Step 2: create /opt/ban-bot directory
echo "Ensuring app directory: ${APP_DIR}"
mkdir -p "${APP_DIR}"
chown root:"${APP_USER}" "${APP_DIR}"
chmod 750 "${APP_DIR}"

# Step 3: copy binary
echo "Installing binary: ${APP_DIR}/${BINARY}"
cp "${BINARY}" "${APP_DIR}/${BINARY}"
chown root:root "${APP_DIR}/${BINARY}"
chmod 755 "${APP_DIR}/${BINARY}"

# Step 4: copy env file only if missing, stripping 'export ' prefix for systemd
if [[ ! -f "${ENV_FILE}" ]]; then
	echo "Creating env file from .envrc: ${ENV_FILE}"
	if [[ -f ".envrc" ]]; then
		sed 's/^export //' .envrc > "${ENV_FILE}"
	else
		echo "Warning: .envrc not found; creating empty env file"
		touch "${ENV_FILE}"
	fi
	chown root:"${APP_USER}" "${ENV_FILE}"
	chmod 640 "${ENV_FILE}"
else
	echo "Env file already exists, skipping: ${ENV_FILE}"
fi

# Step 5: install systemd service
echo "Installing systemd service: ${SYSTEMD_SERVICE}"
cat > "${SYSTEMD_SERVICE}" <<EOF
[Unit]
Description=ban-bot Discord honeypot bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${APP_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${APP_DIR}/${BINARY}
Restart=on-failure
RestartSec=5

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=

[Install]
WantedBy=multi-user.target
EOF

# Step 6: reload and enable
systemctl daemon-reload
if systemctl is-active --quiet ban-bot; then
	echo "Restarting ban-bot"
	systemctl restart ban-bot
else
	echo "Starting and enabling ban-bot"
	systemctl enable --now ban-bot
fi

echo "Install complete."
