# Grimnir Radio Wiki

Welcome to the Grimnir Radio documentation. Grimnir Radio is a modern, production-ready broadcast automation system built in Go with multi-instance scaling, comprehensive observability, and flexible deployment options.

## 🚀 Quick Links

- **[Getting Started](Getting-Started)** - Install and run Grimnir Radio in minutes
- **[Installation Guide](Installation)** - Docker, Nix, and source installation
- **[Architecture Overview](Architecture)** - System design and components
- **[API Reference](API-Reference)** - Complete REST API documentation
- **[Configuration Guide](Configuration)** - Configure Grimnir Radio
- **[Production Deployment](Production-Deployment)** - Deploy to production
- **[Migration Guide](Migration-Guide)** - Migrate from AzuraCast/LibreTime

## 📋 Features

### Core Features
- **Smart Blocks** - Rule-based playlist generation with criteria filtering
- **Clock Scheduling** - Hour templates with repeating patterns
- **Live Broadcasting** - DJ handover with priority system
- **Webstream Relay** - HTTP stream relay with automatic failover
- **Multi-Mount Support** - Multiple output streams per station
- **Media Management** - Local filesystem or S3-compatible storage

### Enterprise Features
- **Multi-Instance Scaling** - Horizontal scaling with Redis/NATS
- **Leader Election** - Automatic leader election for scheduler
- **Health Monitoring** - Prometheus metrics and OpenTelemetry tracing
- **Event Bus** - Distributed events with Redis or NATS JetStream
- **5-Tier Priority System** - Emergency, Live Override, Live Scheduled, Automation, Fallback

## 🏗️ Architecture

Grimnir Radio uses a modern microservices-inspired architecture:

```
┌─────────────────┐     ┌──────────────────┐
│  Control Plane  │────▶│  Media Engine    │
│   (REST API)    │gRPC │   (GStreamer)    │
└─────────────────┘     └──────────────────┘
         │                       │
         ├──────────┬────────────┤
         ▼          ▼            ▼
    ┌────────┐ ┌────────┐  ┌──────────┐
    │  RDBMS │ │  Redis │  │  Icecast │
    └────────┘ └────────┘  └──────────┘
```

- **Control Plane**: Go HTTP API, scheduler, authentication, WebSocket events
- **Media Engine**: GStreamer-based audio processing and streaming (separate process)
- **Storage**: PostgreSQL/MySQL/SQLite for metadata, filesystem/S3 for media files
- **Event Bus**: Redis or NATS for multi-instance coordination

## 📚 Documentation Sections

### Getting Started
- [Quick Start Guide](Getting-Started)
- [Installation Methods](Installation)
- [First Station Setup](Getting-Started#first-station)
- [Development Environment](Development)

### Core Concepts
- [Architecture Overview](Architecture)
- [Smart Blocks](Smart-Blocks)
- [Clock Scheduling](Clock-Scheduling)
- [Priority System](Priority-System)
- [Live Broadcasting](Live-Broadcasting)

### Administration
- [Configuration Reference](Configuration)
- [Production Deployment](Production-Deployment)
- [Multi-Instance Setup](Multi-Instance)
- [Monitoring & Observability](Observability)
- [Database Optimization](Database-Optimization)

### Integration
- [API Reference](API-Reference)
- [WebSocket Events](WebSocket-Events)
- [Migration Guide](Migration-Guide)
- [Output Encoding](Output-Encoding)

### Operations
- [Troubleshooting](Troubleshooting)
- [Performance Tuning](Performance-Tuning)
- [Backup & Restore](Backup-Restore)
- [Upgrading](Upgrading)

## 🤝 Community

- **GitHub**: [friendsincode/grimnir_radio](https://github.com/friendsincode/grimnir_radio)
- **Issues**: [Report bugs or request features](https://github.com/friendsincode/grimnir_radio/issues)
- **License**: AGPL-3.0-or-later

## 📝 Version Information

- **Current Version**: 1.0.0 (Production Release)
- **Latest Update**: 2026-01-23
- **Status**: Production Ready ✅
- **Next Version**: 1.1.0 (Feature Enhancement) - In Progress

See [CHANGELOG](CHANGELOG) for version history and [Roadmap](Roadmap) for upcoming features.

## 🛠️ Technology Stack

- **Language**: Go 1.24+
- **Media Processing**: GStreamer 1.0
- **Database**: PostgreSQL 12+ (or MySQL 8+, SQLite 3.35+)
- **Event Bus**: Redis 6+ or NATS 2.9+
- **Object Storage**: S3-compatible (AWS S3, MinIO, Spaces, B2)
- **Streaming**: Icecast2 / SHOUTcast
- **Observability**: Prometheus, OpenTelemetry

## 📖 Additional Resources

- [API Reference](API-Reference) - Complete REST API documentation
- [Engineering Spec](Engineering-Spec) - Detailed technical specification
- [Sales Spec](Sales-Spec) - Feature comparison and positioning
- [CHANGELOG](CHANGELOG) - Version history and release notes
