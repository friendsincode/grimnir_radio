# Grimnir Radio Roadmap

## Version 1.7.0 (Current)

### Core Features (Complete)
- ✅ Core radio automation functionality
- ✅ Media library management
- ✅ Playlist and smart block creation
- ✅ Schedule management with clock templates
- ✅ Live DJ support with handover
- ✅ Multi-station support
- ✅ Basic analytics
- ✅ 5-tier priority system
- ✅ GStreamer media engine with DSP
- ✅ Multi-instance scaling
- ✅ Full observability (Prometheus, OpenTelemetry)

### Phase 8: Advanced Scheduling (Complete)
- ✅ Shows with RRULE recurrence patterns
- ✅ Visual calendar UI with drag-and-drop
- ✅ Schedule validation and conflict detection
- ✅ Schedule templates and versioning
- ✅ DJ self-service (availability, requests)
- ✅ Notification system (in-app, email)
- ✅ Public schedule API and embeddable widgets
- ✅ Schedule analytics and suggestions
- ✅ Network syndication
- ✅ Underwriting/sponsor management
- ✅ iCal export

---

## Version 1.8.0 (Planned)

### Audio Fingerprinting & Duplicate Detection
**Priority:** High
**Status:** Planned

Implement audio fingerprinting to detect true duplicate audio files regardless of metadata differences.

#### Features:
- [ ] Generate audio fingerprints on upload using Chromaprint/AcoustID
- [ ] Store fingerprints in database for comparison
- [ ] Compare new uploads against existing fingerprints
- [ ] Admin dashboard section: "Duplicate Media Review"
  - List potential duplicates with similarity scores
  - Side-by-side comparison (metadata, duration, bitrate, etc.)
  - Actions: Keep both, merge metadata, delete duplicate
- [ ] Background job to scan existing library for duplicates
- [ ] Configurable similarity threshold
- [ ] Option to auto-reject exact duplicates on upload

#### Technical Notes:
- Use `fpcalc` (Chromaprint CLI) for fingerprint generation
- Store fingerprint as binary/base64 in `media_items.fingerprint` column
- New table: `duplicate_candidates` (media_id_a, media_id_b, similarity_score, status, reviewed_at, reviewed_by)
- Add to media analysis pipeline (after upload)

#### Admin UI:
- New menu item: Dashboard > Admin > Duplicate Detection
- Summary stats: X potential duplicates found, Y reviewed, Z pending
- Filterable list with audio preview for both tracks

---

### Other 1.8.0 Features
- [ ] Bulk media import from folder
- [ ] Improved waveform visualization with zoom
- [ ] Enhanced analytics with export to CSV
- [ ] Audit logging for sensitive operations

---

## Version 2.0.0 (Future)

- [ ] Multi-tenant support (separate organizations)
- [ ] Advanced audio processing (normalization presets)
- [ ] Mobile-responsive DJ controls
- [ ] Integration with external music databases (MusicBrainz)
- [ ] Podcast/show management module
- [ ] Archive recording and on-demand playback
- [ ] DSP graph UI (visual audio processing)
- [ ] WebDJ interface
- [ ] Emergency Alert System (EAS) integration
- [ ] Exhaustive open source dependency audit

---

## Contributing

To contribute to any roadmap item, please open an issue first to discuss the implementation approach.
