# Grimnir Radio Roadmap

**Current Version:** 1.7.0
**Last Updated:** 2026-02-01

---

## Version 1.7.0 (Current Release)

### Core Features (Complete)
- Core radio automation functionality
- Media library management with S3 support
- Playlist and smart block creation
- Schedule management with clock templates
- Live DJ support with handover
- Multi-station support
- 5-tier priority system
- GStreamer media engine with DSP
- Multi-instance scaling with Redis/NATS
- Full observability (Prometheus, OpenTelemetry)
- Turn-key Docker Compose deployment
- Nix packages (Basic, Full, Dev flavors)
- Migration tools (AzuraCast, LibreTime)

### Phase 8: Advanced Scheduling (Complete)

All Phase 8 milestones have been implemented:

| Phase | Feature | Status |
|-------|---------|--------|
| 8A | Shows with RRULE recurrence | Complete |
| 8B | Visual calendar UI | Complete |
| 8C | Validation & conflict detection | Complete |
| 8D | Templates & versioning | Complete |
| 8E | DJ self-service | Complete |
| 8F | Notifications | Complete |
| 8G | Public schedule API & widgets | Complete |
| 8H | Analytics, syndication, underwriting | Complete |

**Key Features:**
- Recurring shows with RFC 5545 RRULE patterns
- Visual calendar with drag-and-drop scheduling
- Schedule conflict detection and validation rules
- Save/restore schedule templates with version history
- DJ availability management and shift requests
- In-app and email notifications
- Public schedule API and embeddable widgets
- Schedule analytics with performance metrics
- Network syndication across stations
- Underwriting/sponsor management
- iCal export for calendar integration

---

## Version 1.8.0 (Planned)

### Audio Fingerprinting & Duplicate Detection
**Priority:** High

Implement audio fingerprinting to detect true duplicate audio files regardless of metadata differences.

**Features:**
- Generate audio fingerprints on upload using Chromaprint/AcoustID
- Store fingerprints in database for comparison
- Admin dashboard for duplicate review
- Side-by-side comparison with audio preview
- Configurable similarity threshold
- Auto-reject exact duplicates option

### Other 1.8.0 Features
- Bulk media import from folder
- Improved waveform visualization with zoom
- Enhanced analytics with CSV export
- Audit logging for sensitive operations

---

## Version 2.0.0 (Future)

- Multi-tenant support (separate organizations)
- Advanced audio processing presets
- Mobile-responsive DJ controls
- Integration with MusicBrainz
- Podcast/show management module
- Archive recording and on-demand playback
- DSP graph visual editor
- WebDJ interface
- Emergency Alert System (EAS) integration

---

## Recent Releases

### 1.7.0 (2026-02-01)
- Complete Phase 8 Advanced Scheduling
- 50+ new API endpoints
- 21 new database tables
- Schedule analytics and syndication
- Underwriting management

### 1.3.0 (2026-02-01)
- API key authentication (replaced JWT login)
- API key management UI

### 1.0.0 (2026-01-22)
- Production-ready release
- All core phases complete (0, 4A-4C, 5-7)

---

## Contributing

To contribute to any roadmap item, please open an issue first to discuss the implementation approach.

See [Contributing](https://github.com/friendsincode/grimnir_radio/blob/main/CONTRIBUTING.md) for guidelines.
