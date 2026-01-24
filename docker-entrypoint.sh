#!/bin/sh
set -e

# Fix ownership of media directory if mounted as volume
# This runs as root before dropping to grimnir user
if [ "$(id -u)" = "0" ]; then
    # Fix permissions on mounted volumes
    chown -R grimnir:grimnir /var/lib/grimnir 2>/dev/null || true

    # Drop to grimnir user and exec the command
    exec su-exec grimnir "$@"
else
    # Already running as grimnir user
    exec "$@"
fi
