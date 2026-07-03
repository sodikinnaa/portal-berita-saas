#!/bin/bash
set -e

# Configuration
APP_DIR="/home/website/portal-berita/porta-berita"
NGINX_CONF="/etc/nginx/conf.d/news.meowcing.my.id.conf"

echo "Checking currently active environment..."

# Detect active port from Nginx config
if grep -q "127.0.0.1:8082" "$NGINX_CONF"; then
    ACTIVE_COLOR="blue"
    ACTIVE_PORT="8082"
    STANDBY_COLOR="green"
    STANDBY_PORT="8083"
elif grep -q "127.0.0.1:8083" "$NGINX_CONF"; then
    ACTIVE_COLOR="green"
    ACTIVE_PORT="8083"
    STANDBY_COLOR="blue"
    STANDBY_PORT="8082"
else
    echo "Error: Could not detect active port from Nginx config."
    exit 1
fi

echo "Active environment: $ACTIVE_COLOR (Port $ACTIVE_PORT)"
echo "Standby environment: $STANDBY_COLOR (Port $STANDBY_PORT)"

echo "Building new Go binary..."
cd "$APP_DIR"
go build -o portal-new ./cmd/portal

echo "Copying binary to $STANDBY_COLOR environment..."
cp portal-new "portal-$STANDBY_COLOR"

echo "Starting $STANDBY_COLOR service..."
sudo systemctl restart "portal-$STANDBY_COLOR"

echo "Waiting for $STANDBY_COLOR service to start..."
HEALTHY=false
for i in {1..10}; do
    if curl -s -f "http://127.0.0.1:$STANDBY_PORT/healthz" > /dev/null; then
        echo "$STANDBY_COLOR service is healthy!"
        HEALTHY=true
        break
    fi
    echo "Waiting... ($i/10)"
    sleep 1
done

if [ "$HEALTHY" = true ]; then
    echo "Health check passed. Updating Nginx configuration..."
    # Switch Nginx to standby port
    sudo sed -i "s/127.0.0.1:$ACTIVE_PORT/127.0.0.1:$STANDBY_PORT/g" "$NGINX_CONF"
    
    echo "Reloading Nginx..."
    sudo systemctl reload nginx
    
    echo "Stopping old $ACTIVE_COLOR service..."
    sudo systemctl stop "portal-$ACTIVE_COLOR"
    
    # Cleanup temporary binary
    rm -f portal-new
    
    echo "Deploy successful! Switched traffic to $STANDBY_COLOR (Port $STANDBY_PORT) with zero downtime."
else
    echo "Error: Health check failed on standby port $STANDBY_PORT."
    echo "Aborting deployment. Stopping $STANDBY_COLOR service."
    sudo systemctl stop "portal-$STANDBY_COLOR"
    rm -f portal-new
    exit 1
fi
