#!/bin/bash
# Grimnir Radio - Nginx Setup Script
#
# Usage: ./setup.sh radio.yourdomain.com

set -e

DOMAIN=${1:-"radio.example.com"}
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ "$EUID" -ne 0 ]; then
    echo "Please run as root (sudo ./setup.sh $DOMAIN)"
    exit 1
fi

if [ "$DOMAIN" = "radio.example.com" ]; then
    echo "Usage: sudo ./setup.sh radio.yourdomain.com"
    exit 1
fi

echo "Setting up Nginx for Grimnir Radio..."
echo "Domain: $DOMAIN"

# Create certbot webroot
mkdir -p /var/www/certbot

# Copy and configure nginx config
sed "s/radio.yourdomain.com/$DOMAIN/g" "$SCRIPT_DIR/grimnir-radio.conf" > /etc/nginx/sites-available/grimnir-radio.conf

# Enable site
ln -sf /etc/nginx/sites-available/grimnir-radio.conf /etc/nginx/sites-enabled/

# Test nginx config
nginx -t

# Reload nginx
systemctl reload nginx

echo ""
echo "Nginx configured for HTTP. Now run certbot to enable HTTPS:"
echo ""
echo "  certbot --nginx -d $DOMAIN"
echo ""
echo "After certbot completes, edit /etc/nginx/sites-available/grimnir-radio.conf:"
echo "  1. Uncomment the HTTPS server block"
echo "  2. Uncomment the HTTP->HTTPS redirect"
echo "  3. Reload nginx: systemctl reload nginx"
echo ""
