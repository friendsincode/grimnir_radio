# Grimnir Radio - Multi-Instance Deployment Guide

**Version:** 1.0  
**Last Updated:** 2026-01-22

This guide covers horizontal scaling and multi-instance deployment of Grimnir Radio for high availability and load distribution.

See full documentation at: https://github.com/friendsincode/grimnir_radio/blob/main/docs/MULTI_INSTANCE.md

## Key Features

- **Consistent Hashing** - Deterministic executor-to-instance assignment
- **Leader Election** - Redis-based scheduler coordination
- **Auto-Rebalancing** - Executors migrate when instances join/leave
- **Minimal Churn** - Only ~25% of executors move during topology changes

## Quick Start

```bash
# Start 3-instance cluster
docker-compose up -d postgres redis
docker-compose up -d grimnir-1 grimnir-2 grimnir-3

# Verify cluster health
curl http://localhost:8081/healthz  # Check leader status
curl http://localhost:8081/api/v1/executors/list  # Check distribution
```

See PRODUCTION_DEPLOYMENT.md and OBSERVABILITY.md for complete setup instructions.
