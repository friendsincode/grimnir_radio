#!/bin/bash
# Configure pg_hba.conf to allow connections from Docker network

set -e

# Allow connections from any host with password authentication
cat >> "$PGDATA/pg_hba.conf" << EOF

# Allow connections from Docker network (scram-sha-256 authentication)
host    all             all             0.0.0.0/0               scram-sha-256
host    all             all             ::/0                    scram-sha-256
EOF

echo "pg_hba.conf configured for Docker network access"
