#!/bin/bash
set -e

echo "Setting up Blue-Green Systemd services on VPS..."

# 1. Create Blue service
sudo bash -c 'cat > /etc/systemd/system/portal-blue.service <<EOF
[Unit]
Description=Porta Berita News Portal CMS (Blue)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/home/website/portal-berita/porta-berita
ExecStart=/home/website/portal-berita/porta-berita/portal-blue
Restart=always
RestartSec=5
Environment=PATH=/usr/bin:/usr/local/bin ADDR=:8082
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=portal-blue

[Install]
WantedBy=multi-user.target
EOF'

# 2. Create Green service
sudo bash -c 'cat > /etc/systemd/system/portal-green.service <<EOF
[Unit]
Description=Porta Berita News Portal CMS (Green)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/home/website/portal-berita/porta-berita
ExecStart=/home/website/portal-berita/porta-berita/portal-green
Restart=always
RestartSec=5
Environment=PATH=/usr/bin:/usr/local/bin ADDR=:8083
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=portal-green

[Install]
WantedBy=multi-user.target
EOF'

echo "Reloading systemd daemon..."
sudo systemctl daemon-reload

echo "Enabling portal-blue and portal-green services..."
sudo systemctl enable portal-blue
sudo systemctl enable portal-green

# Stop old portal service if running, and start portal-blue
if systemctl is-active --quiet portal; then
    echo "Stopping old portal.service and starting portal-blue.service..."
    sudo systemctl stop portal
    sudo systemctl disable portal || true
fi

# Ensure portal-blue binary exists
if [ ! -f "/home/website/portal-berita/porta-berita/portal-blue" ]; then
    echo "Copying existing binary to portal-blue..."
    cp /home/website/portal-berita/porta-berita/portal /home/website/portal-berita/porta-berita/portal-blue
fi

echo "Starting portal-blue.service..."
sudo systemctl restart portal-blue

echo "Setup completed successfully! Blue-Green deployment architecture is ready."
