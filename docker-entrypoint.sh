#!/bin/sh
set -e

# Fix ownership of media directory if mounted as volume
# This runs as root before dropping to grimnir user
if [ "$(id -u)" = "0" ]; then
    # Fix permissions on mounted volumes only if ownership is wrong
    # This avoids slow recursive chown on large media libraries
    if [ "$(stat -c %U /var/lib/grimnir 2>/dev/null)" != "grimnir" ]; then
        chown -R grimnir:grimnir /var/lib/grimnir 2>/dev/null || true
    fi

    # Drop to grimnir user and exec the command
    exec su-exec grimnir "$@"
else
    # Already running as grimnir user
    exec "$@"
fi
