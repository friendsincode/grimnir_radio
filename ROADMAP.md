# Grimnir Radio Roadmap

## Version 1.0.0 (Current)
- Core radio automation functionality
- Media library management
- Playlist and smart block creation
- Schedule management with clock templates
- Live DJ support with handover
- Multi-station support
- Basic analytics

---

## Version 1.1.0 (Planned)

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

### Other 1.1.0 Features
- [ ] Bulk media import from folder
- [ ] Improved waveform visualization with zoom
- [ ] Playlist scheduling rules (day-parting)
- [ ] Enhanced analytics with export to CSV
- [ ] Webhook notifications for events

---

## Version 1.2.0 (Future)

- [ ] Multi-tenant support (separate organizations)
- [ ] Advanced audio processing (normalization presets)
- [ ] Mobile-responsive DJ controls
- [ ] Integration with external music databases (MusicBrainz)
- [ ] Podcast/show management module
- [ ] Archive recording and on-demand playback

---

## Contributing

To contribute to any roadmap item, please open an issue first to discuss the implementation approach.
