# RLMradio.xyz-AI: Optimized Station Blueprint

> Built from studying the existing RLMradio.xyz schedule entry-by-entry.
> Same shows, same time slots — smart blocks replace playlists, gotchas fixed.

---

## Hard-Set Rules (Non-Negotiable)

| Item | Day | Time (Central) | Type |
|------|-----|----------------|------|
| **Liberty Tunes LIVE** (Unkle Bonehead) | Friday | 7:00 PM – 9:00 PM | Webstream relay |
| **Liberty Tunes LIVE** (Unkle Bonehead) | Saturday | 2:00 PM – 4:00 PM | Webstream relay |
| **Behind the Woodshed LIVE** (Hal) | Sunday | 1:50 PM – 4:00 PM | Webstream relay + fallback |
| **Behind the Woodshed REPLAY** | Sunday | 7:00 PM – 9:00 PM | Exact recording playback |
| **Advertisements** | Daily | Between episodes | Interstitial underwriting spots |

---

## Gotchas Found in Current Schedule (and How They're Fixed)

| # | Problem | Fix |
|---|---------|-----|
| 1 | **UTC/Central confusion** — entries stored in UTC but day-of-week assigned by UTC day, so "Monday 03:00 UTC" = Sunday 9PM CT. Shows land on the wrong day. | All entries rebuilt in proper Central Time with correct day assignments. |
| 2 | **Tuesday Vin-E runs 7 HOURS** (12PM-7PM CT) — single clock template playing a 121-item playlist on loop for 7 hours. | Split into normal 2-hour block. Afternoon filled with appropriate shows. |
| 3 | **Tuesday Vin-E and Mixed Tyranny-1 OVERLAP** from 2-7PM CT. Two entries playing simultaneously. | Eliminated overlap. Each show gets its own clean slot. |
| 4 | **Thursday Mixed Tyranny-1 runs 5 HOURS** (2-7PM CT). | Split into appropriate show blocks. |
| 5 | **Sunday 6AM-1PM is one 7-hour Mixed Tyranny block.** | Broken into proper show blocks matching the rest of the week's pattern. |
| 6 | **Monday & Saturday have NO Hal PM** — 7-9PM gap with nothing scheduled. | Added Hal PM to fill the gap (smart block has deep enough catalog). |
| 7 | **2 entries have empty recurrence_days []** — Hal PM and Unkle Bonehead entries that never fire. | Deleted. Replaced with properly configured entries. |
| 8 | **Unkle Bonehead webstream marked inactive** but still scheduled. | Replaced with Liberty Tunes webstream (active, internal). |
| 9 | **Hal Sunday replay uses a 3-item playlist** that loops. | Replaced with actual recording playback of the live broadcast. |
| 10 | **All clock templates wrap playlists** — small pools loop endlessly within long slots. | Smart blocks with title/artist separation. No more looping. |
| 11 | **Sunday "BEFORE Hal" slot starts at 1PM but BTW starts at 1:50PM** — 52-minute filler block. | Clean 12-1:50PM pre-show block. |
| 12 | **92 entries, no recurrence_end_date on any** — all run forever even if obsolete. | New entries get end dates. Old ones deleted. |

---

## Station Settings

```
Name:              RLMradio.xyz
Timezone:          America/Chicago
Schedule Boundary: hard
Crossfade:         disabled (talk-heavy format)
Artist Separation: 60 min (station default)
Title Separation:  180 min (station default)
Fallback:          Smart Block "Station Fill"
```

---

## Smart Blocks (Replace All Playlists)

### Talk Show Blocks

| Smart Block | Filter | Title Sep | Target | Interstitial |
|---|---|---|---|---|
| **Hal Anthony AM** | artist "Hal Anthony" | 7 days | 120 min | 60s ad |
| **Hal Anthony PM** | artist "Hal Anthony" | 1 day | 120 min | 60s ad |
| **Dork Table** | text "Dork Table" | 3 days | 120 min | 60s ad |
| **Doc Mike** | artist "Doc Mike" | 2 days | 60 min | 60s ad |
| **Grim Leftovers** | text "Grim Leftovers" | 3 days | 60 min | 60s ad |
| **Grim All Connected** | text "All Connected" | 3 days | 60 min | 60s ad |
| **Grim All Connected PM** | text "All Connected" | 2 days | 60 min | 60s ad |
| **Road Less Traveled** | text "Road Less Traveled" | 3 days | 60 min | 60s ad |
| **Dropping A Coil** | text "Dropping" | 1 day | 120 min | 60s ad |
| **Freakers Ball** | text "Freaker" | 3 days | 120 min | 60s ad |
| **Grammy's Rocket Chair** | artist "Grammy" or "Gammy" | 2 days | 60 min | 60s ad |
| **Vin-E / Ponder Gander** | artist "Vin-E" | 2 days | 120 min | 60s ad |
| **Military State** | text "Military State" or "Pat Jordan" or "Democracy Weapon" | 1 day | 180 min | 60s ad |
| **Mixed Tyranny** | text "Tyranny" or related | 1 day | 120 min | 60s ad |
| **Truth Stream Media** | artist "Truth Stream" | 3 days | 120 min | 60s ad |
| **Jeff Mattox** | artist "Jeff Mattox" | 3 days | 120 min | 60s ad |
| **Brady Police Record** | text "Brady" or "Police Record" | 3 days | 60 min | 60s ad |
| **Free Your Mind** | text "Free Your Mind" or "Moosegurl" | 3 days | 60 min | 60s ad |
| **Top Ten Countdown** | text "Top Ten" or genre "Oldies" | 3 days | 60 min | 60s ad |
| **Art of Craig S.** | artist "Craig" | 7 days | 120 min | 60s ad |
| **Unkle Bonehead 24/7** | artist "Unkle Bonehead" or "Bonehead" | 3 days | 120 min | 60s ad |

### Special Blocks

| Smart Block | Filter | Notes |
|---|---|---|
| **BTW Fallback (Fresh Tracks)** | **Newer than 30 days** | Fallback if Hal's stream drops. Only plays tracks uploaded in the last month. |
| **Station Fill** | All tracks, random, artist sep 60 min, title sep 180 min | Catch-all fallback for any gap. Pulls from entire 2,433-track library. |
| **Station Tunes** | Short music tracks (< 8 min), genre "Music" or "Oldies" or untagged | Music filler for pre-show ramps and music hours. Uses clock template with :28/:58 ad breaks. |

---

## Weekly Schedule Grid (All Times Central)

### MONDAY

| Time | Show | Source Type | Notes |
|------|------|------------|-------|
| 12:00 AM – 2:00 AM | Dork Table | Smart Block | Daily anchor |
| 2:00 AM – 3:00 AM | Grim Leftovers | Smart Block | MWF |
| 3:00 AM – 4:00 AM | Road Less Traveled | Smart Block | MWF |
| 4:00 AM – 5:00 AM | Doc Mike | Smart Block | Daily |
| 5:00 AM – 6:00 AM | Station Tunes | Clock Template | Daily, :28/:58 ads |
| 6:00 AM – 8:00 AM | Freakers Ball | Smart Block | Mon/Tue |
| 8:00 AM – 10:00 AM | Hal Anthony AM | Smart Block | Mon–Sat |
| 10:00 AM – 12:00 PM | Dropping A Coil | Smart Block | Daily |
| 12:00 PM – 2:00 PM | Vin-E / Ponder Gander | Smart Block | MWF |
| 2:00 PM – 4:00 PM | Grammy's Rocket Chair | Smart Block | MWF |
| 4:00 PM – 7:00 PM | Mixed Tyranny | Smart Block | **Was gap filler, kept** |
| 7:00 PM – 9:00 PM | Hal Anthony PM | Smart Block | **FIX: was missing Mon** |
| 9:00 PM – 12:00 AM | Military State | Smart Block | Nightly |

### TUESDAY

| Time | Show | Source Type | Notes |
|------|------|------------|-------|
| 12:00 AM – 2:00 AM | Dork Table | Smart Block | Daily |
| 2:00 AM – 3:00 AM | Grim All Connected | Smart Block | TuThSatSun |
| 3:00 AM – 4:00 AM | Brady Police Record | Smart Block | TuThSat |
| 4:00 AM – 5:00 AM | Doc Mike | Smart Block | Daily |
| 5:00 AM – 6:00 AM | Station Tunes | Clock Template | Daily |
| 6:00 AM – 8:00 AM | Freakers Ball | Smart Block | Mon/Tue |
| 8:00 AM – 10:00 AM | Hal Anthony AM | Smart Block | Mon–Sat |
| 10:00 AM – 12:00 PM | Dropping A Coil | Smart Block | Daily |
| 12:00 PM – 2:00 PM | Vin-E / Ponder Gander | Smart Block | **FIX: was 7 hours, now 2** |
| 2:00 PM – 4:00 PM | Jeff Mattox | Smart Block | Tue/Sat afternoons |
| 4:00 PM – 7:00 PM | Mixed Tyranny | Smart Block | **FIX: was overlapping** |
| 7:00 PM – 9:00 PM | Hal Anthony PM | Smart Block | |
| 9:00 PM – 10:00 PM | Brady Police Record DOT Com | Smart Block | TuThSat |
| 10:00 PM – 11:00 PM | Top Ten Countdown | Smart Block | Tue/Sat |
| 11:00 PM – 12:00 AM | Grim All Connected PM | Smart Block | TuThSat |

### WEDNESDAY

| Time | Show | Source Type | Notes |
|------|------|------------|-------|
| 12:00 AM – 2:00 AM | Dork Table | Smart Block | Daily |
| 2:00 AM – 3:00 AM | Grim Leftovers | Smart Block | MWF |
| 3:00 AM – 4:00 AM | Road Less Traveled | Smart Block | MWF |
| 4:00 AM – 5:00 AM | Doc Mike | Smart Block | Daily |
| 5:00 AM – 6:00 AM | Station Tunes | Clock Template | Daily |
| 6:00 AM – 8:00 AM | Truth Stream Media | Smart Block | Wed/Fri |
| 8:00 AM – 10:00 AM | Hal Anthony AM | Smart Block | Mon–Sat |
| 10:00 AM – 12:00 PM | Dropping A Coil | Smart Block | Daily |
| 12:00 PM – 2:00 PM | Vin-E / Ponder Gander | Smart Block | MWF |
| 2:00 PM – 4:00 PM | Grammy's Rocket Chair | Smart Block | MWF |
| 4:00 PM – 7:00 PM | Mixed Tyranny | Smart Block | |
| 7:00 PM – 9:00 PM | Hal Anthony PM | Smart Block | |
| 9:00 PM – 10:00 PM | Brady Police Record DOT Com | Smart Block | |
| 10:00 PM – 11:00 PM | Free Your Mind | Smart Block | Wed/Thu |
| 11:00 PM – 12:00 AM | Grim All Connected PM | Smart Block | |

### THURSDAY

| Time | Show | Source Type | Notes |
|------|------|------------|-------|
| 12:00 AM – 2:00 AM | Dork Table | Smart Block | Daily |
| 2:00 AM – 3:00 AM | Grim All Connected | Smart Block | TuThSatSun |
| 3:00 AM – 4:00 AM | Brady Police Record | Smart Block | TuThSat |
| 4:00 AM – 5:00 AM | Doc Mike | Smart Block | Daily |
| 5:00 AM – 6:00 AM | Station Tunes | Clock Template | Daily |
| 6:00 AM – 8:00 AM | Jeff Mattox | Smart Block | **Thu/Sat (matched existing)** |
| 8:00 AM – 10:00 AM | Hal Anthony AM | Smart Block | Mon–Sat |
| 10:00 AM – 12:00 PM | Dropping A Coil | Smart Block | Daily |
| 12:00 PM – 2:00 PM | Vin-E / Ponder Gander | Smart Block | |
| 2:00 PM – 4:00 PM | Art of Craig S. | Smart Block | **FIX: was 5hr Mixed Tyranny** |
| 4:00 PM – 7:00 PM | Mixed Tyranny | Smart Block | |
| 7:00 PM – 9:00 PM | Hal Anthony PM | Smart Block | |
| 9:00 PM – 10:00 PM | Brady Police Record DOT Com | Smart Block | |
| 10:00 PM – 11:00 PM | Free Your Mind | Smart Block | Wed/Thu |
| 11:00 PM – 12:00 AM | Grim All Connected PM | Smart Block | |

### FRIDAY

| Time | Show | Source Type | Notes |
|------|------|------------|-------|
| 12:00 AM – 2:00 AM | Dork Table | Smart Block | Daily |
| 2:00 AM – 3:00 AM | Grim Leftovers | Smart Block | MWF |
| 3:00 AM – 4:00 AM | Road Less Traveled | Smart Block | MWF |
| 4:00 AM – 5:00 AM | Doc Mike | Smart Block | Daily |
| 5:00 AM – 6:00 AM | Station Tunes | Clock Template | Daily |
| 6:00 AM – 8:00 AM | Truth Stream Media | Smart Block | Wed/Fri |
| 8:00 AM – 10:00 AM | Hal Anthony AM | Smart Block | Mon–Sat |
| 10:00 AM – 12:00 PM | Dropping A Coil | Smart Block | Daily |
| 12:00 PM – 2:00 PM | Vin-E Friday | Smart Block | |
| 2:00 PM – 4:00 PM | Grammy's Rocket Chair | Smart Block | MWF |
| 4:00 PM – 5:00 PM | RLM Tunes / Dono | Smart Block | **Existing: station promos** |
| 5:00 PM – 7:00 PM | Art of Craig S. | Smart Block | **Existing slot** |
| **7:00 PM – 9:00 PM** | **LIBERTY TUNES LIVE** | **Webstream** | **HARD SET** |
| 9:00 PM – 10:00 PM | Brady Police Record DOT Com | Smart Block | |
| 10:00 PM – 11:00 PM | Top Ten Countdown | Smart Block | Fri/Sat |
| 11:00 PM – 12:00 AM | Grim All Connected PM | Smart Block | |

### SATURDAY

| Time | Show | Source Type | Notes |
|------|------|------------|-------|
| 12:00 AM – 2:00 AM | Dork Table | Smart Block | Daily |
| 2:00 AM – 3:00 AM | Grim All Connected | Smart Block | TuThSatSun |
| 3:00 AM – 4:00 AM | Brady Police Record | Smart Block | TuThSat |
| 4:00 AM – 5:00 AM | Doc Mike | Smart Block | Daily |
| 5:00 AM – 6:00 AM | Station Tunes | Clock Template | Daily |
| 6:00 AM – 8:00 AM | Jeff Mattox | Smart Block | **Thu/Sat (matched existing)** |
| 8:00 AM – 10:00 AM | Hal Anthony AM | Smart Block | Mon–Sat |
| 10:00 AM – 12:00 PM | Dropping A Coil | Smart Block | Daily |
| 12:00 PM – 2:00 PM | Vin-E / Ponder Gander | Smart Block | |
| **2:00 PM – 4:00 PM** | **LIBERTY TUNES LIVE** | **Webstream** | **HARD SET** |
| 4:00 PM – 7:00 PM | Mixed Tyranny | Smart Block | |
| 7:00 PM – 9:00 PM | Hal Anthony PM | Smart Block | **FIX: was empty (broken entry)** |
| 9:00 PM – 10:00 PM | Top Ten Countdown | Smart Block | Tue/Sat |
| 10:00 PM – 11:00 PM | Grim All Connected PM | Smart Block | |
| 11:00 PM – 12:00 AM | Military State | Smart Block | |

### SUNDAY

| Time | Show | Source Type | Notes |
|------|------|------------|-------|
| 12:00 AM – 2:00 AM | Dork Table | Smart Block | Daily |
| 2:00 AM – 3:00 AM | Grim All Connected | Smart Block | TuThSatSun |
| 3:00 AM – 4:00 AM | Road Less Traveled | Smart Block | **Existing had this on Sun** |
| 4:00 AM – 5:00 AM | Doc Mike | Smart Block | Daily |
| 5:00 AM – 6:00 AM | Station Tunes | Clock Template | Daily |
| 6:00 AM – 8:00 AM | Freakers Ball | Smart Block | **FIX: was 7hr Mixed Tyranny** |
| 8:00 AM – 10:00 AM | Hal Anthony AM | Smart Block | |
| 10:00 AM – 12:00 PM | Unkle Bonehead 24/7 | Smart Block | **Sunday fill slot** |
| 12:00 PM – 1:50 PM | Station Tunes (Pre-Show) | Smart Block | **FIX: clean pre-show ramp** |
| **1:50 PM – 4:00 PM** | **BEHIND THE WOODSHED LIVE** | **Webstream + Fallback** | **HARD SET** |
| 4:00 PM – 5:00 PM | Grammy's Rocket Chair | Smart Block | |
| 5:00 PM – 7:00 PM | Mixed Tyranny | Smart Block | |
| **7:00 PM – 9:00 PM** | **BEHIND THE WOODSHED REPLAY** | **Recording (exact)** | **HARD SET** |
| 9:00 PM – 12:00 AM | Military State | Smart Block | Nightly |

---

## Show Pattern Summary

| Show | Days | Time (CT) | Source on Existing |
|------|------|-----------|-------------------|
| Dork Table | Daily | 12–2 AM | ✓ same |
| Grim Leftovers | Mon/Wed/Fri | 2–3 AM | ✓ same |
| Grim All Connected | Tue/Thu/Sat/Sun | 2–3 AM | ✓ same |
| Road Less Traveled | Mon/Wed/Fri/Sun | 3–4 AM | ✓ same (Sun was in existing) |
| Brady Police Record | Tue/Thu/Sat | 3–4 AM | ✓ same |
| Doc Mike | Daily | 4–5 AM | ✓ same |
| Station Tunes | Daily | 5–6 AM | ✓ same |
| Freakers Ball | Mon/Tue/Sun | 6–8 AM | ✓ same (was Mon/Tue, added Sun) |
| Truth Stream Media | Wed/Fri | 6–8 AM | ✓ same |
| Jeff Mattox | Thu/Sat | 6–8 AM | ✓ same |
| Hal Anthony AM | Mon–Sat | 8–10 AM | ✓ same |
| Dropping A Coil | Daily | 10 AM–12 PM | ✓ same |
| Vin-E / Ponder Gander | Mon–Fri (+ Sat) | 12–2 PM | **FIX: Tue was 7hr, now 2hr** |
| Grammy's Rocket Chair | Mon/Wed/Fri/Sun | 2–4 PM or 4–5 PM | ✓ same |
| Jeff Mattox | Tue | 2–4 PM | ✓ same |
| Art of Craig S. | Thu 2–4 PM, Fri 5–7 PM | afternoon | ✓ same |
| RLM Tunes / Dono | Fri | 4–5 PM | ✓ same |
| Mixed Tyranny | Daily | 4–7 PM (varies) | ✓ same (fill role) |
| Hal Anthony PM | Mon–Thu, Sat | 7–9 PM | **FIX: added Mon & Sat** |
| Liberty Tunes LIVE | Fri 7–9 PM, Sat 2–4 PM | — | **FIX: was inactive webstream** |
| Behind the Woodshed LIVE | Sun 1:50–4 PM | — | ✓ same time |
| Behind the Woodshed REPLAY | Sun 7–9 PM | — | **FIX: was 3-item playlist loop** |
| Brady Police Record DOT Com | Tue/Wed/Thu/Fri | 9–10 PM | ✓ same |
| Top Ten Countdown | Tue/Fri/Sat | 10–11 PM | ✓ same |
| Free Your Mind | Wed/Thu | 10–11 PM | ✓ same |
| Grim All Connected PM | Most nights | 11 PM–12 AM | ✓ same |
| Military State | Nightly | 9 PM–12 AM (varies) | ✓ same (fill role) |
| Unkle Bonehead 24/7 | Sun | 10 AM–12 PM | ✓ same |

---

## Behind the Woodshed — Special Handling

### Sunday 1:50 PM – 4:00 PM: LIVE (Syndicated)

```
Primary Source:    Webstream (https://rlmradio.xyz/behind-the-woodshed)
Fallback Source:   Smart Block "BTW Fallback (Fresh Tracks)"
Start:             1:50 PM CT (Hal starts 10 min before the hour)
End:               4:00 PM CT
Recording:         ENABLED — captures whatever airs
```

- Hal broadcasts on his own station (Behind The Woodshed, shortcode `btw`)
- RLMradio.xyz-AI syndicates by pulling `https://rlmradio.xyz/behind-the-woodshed`
- If stream unreachable → fallback plays tracks uploaded in the **last 30 days**
- Everything that airs is recorded

### Sunday 7:00 PM – 9:00 PM: EXACT REPLAY

```
Source:            Recording from the 1:50 PM slot
Playback:          Exact — whatever aired, plays back identically
```

---

## Webstream Configuration

| Name | URL | Schedule |
|------|-----|----------|
| Liberty Tunes (Unkle Bonehead) | `https://rlmradio.xyz/liberty-tunes` | Fri 7–9 PM, Sat 2–4 PM |
| Behind the Woodshed (Hal) | `https://rlmradio.xyz/behind-the-woodshed` | Sun 1:50–4 PM |

Both are internal to the platform — no external dependencies.

---

## Underwriting / Advertisements

**Talk blocks:** Ad interstitials between episodes. Episode finishes → 60s spot → next episode.

**Music hour (Station Tunes 5-6AM):** Clock template with :28/:58 breaks.

**Live webstreams:** No system ads — the live broadcast has its own breaks.

**Replay:** No ads — baked in from the live recording.

### Setup

1. Upload spot audio (30s or 60s files)
2. Create underwriting obligations: sponsor, file, spots/week, preferred dayparts
3. Upload station IDs/promos as fallback for empty ad windows
4. Configure interstitial on all talk smart blocks

---

## Content Pool Protection (Title Separation vs Pool Size)

| Show | Pool | Weekly Airtime | Title Sep | Risk |
|------|------|---------------|-----------|------|
| Art of Craig S. | 13 | 4 hr | 7 days | Low — only 2 slots/week |
| Unkle Bonehead 24/7 | 15 | 2 hr | 3 days | Low — 1 slot/week |
| Top Ten Countdown | 21 | 3 hr | 3 days | OK |
| Free Your Mind | 22 | 2 hr | 3 days | OK |
| Brady Police Record | 36+31+9 = 76 | 7 hr | 3 days | Fine |
| Road Less Traveled | 33 | 4 hr | 3 days | OK |
| Grim Leftovers | 35 | 3 hr | 3 days | OK |
| Grim All Connected | 54 | 7 hr | 3 days | Fine |
| Truth Stream Media | 58 | 4 hr | 3 days | Fine |
| Freakers Ball | 62 | 6 hr | 3 days | Fine |
| Military State | 172 combined | 14 hr | 1 day | Deep pool |
| Mixed Tyranny | 42+89 = 131 | 18 hr | 1 day | Deep combined |
| Vin-E | 121 | 12 hr | 2 days | Fine |
| Doc Mike | 182 | 7 hr | 2 days | Deep |
| Dork Table | 227 | 14 hr | 3 days | Huge pool |
| Jeff Mattox | 246 | 8 hr | 3 days | Huge pool |
| Grammy's Rocket Chair | 267 | 6 hr | 2 days | Biggest pool |

If any block exhausts eligible tracks → falls through to "Station Fill" (2,433 tracks).

---

## Implementation Order

### Phase 1: Build Smart Blocks (20 min)
- [ ] Create all 21 talk smart blocks with correct filters and separation rules
- [ ] Create "BTW Fallback (Fresh Tracks)" — filter: newer than 30 days
- [ ] Create "Station Fill" — catch-all fallback, full library
- [ ] Create "Station Tunes" — music filler for clock template hour
- [ ] Preview each block to confirm it pulls the right content

### Phase 2: Configure Webstreams (5 min)
- [ ] Configure Liberty Tunes webstream (`https://rlmradio.xyz/liberty-tunes`)
- [ ] Add Behind the Woodshed webstream (`https://rlmradio.xyz/behind-the-woodshed`)
- [ ] Set BTW fallback to "BTW Fallback (Fresh Tracks)" smart block

### Phase 3: Enable Recording (5 min)
- [ ] Enable auto-recording for Sunday 1:50 PM slot
- [ ] Verify recording format is FLAC
- [ ] Test recording playback

### Phase 4: Clear Old Schedule (5 min)
- [ ] Delete all 92 existing recurring schedule entries
- [ ] Delete unused/duplicate clock hours (31 of 60 are dead weight)
- [ ] Clean up duplicate smart blocks

### Phase 5: Build New Schedule (30 min)
- [ ] Monday: 13 entries
- [ ] Tuesday: 15 entries
- [ ] Wednesday: 15 entries
- [ ] Thursday: 15 entries
- [ ] Friday: 16 entries (includes Liberty Tunes LIVE)
- [ ] Saturday: 15 entries (includes Liberty Tunes LIVE)
- [ ] Sunday: 14 entries (includes BTW LIVE + REPLAY)
- [ ] Set station fallback to "Station Fill" smart block
- [ ] All entries: weekly recurrence with correct Central Time days

### Phase 6: Underwriting (10 min)
- [ ] Upload sponsor audio files
- [ ] Create obligations with weekly targets and daypart preferences
- [ ] Upload station IDs/promos as fallback
- [ ] Enable interstitial on all talk smart blocks

### Phase 7: Verify (15 min)
- [ ] Calendar view: no gaps, no overlaps, 24/7 coverage all 7 days
- [ ] Smart block previews return correct content
- [ ] Liberty Tunes webstream connects
- [ ] BTW webstream connects
- [ ] Recording triggers on Sunday test
- [ ] Monitor first 24 hours for repetition in play history

---

## Weekly Hours Breakdown

| Content Type | Hours/Week | % |
|---|---|---|
| Talk shows (smart blocks) | ~126 hr | 75.0% |
| Live webstreams (Liberty Tunes + BTW) | ~6 hr 10 min | 3.7% |
| Recording replay (BTW Sunday) | ~2 hr | 1.2% |
| Music / Station Tunes (clock template) | ~7 hr | 4.2% |
| Fill (Military State, Mixed Tyranny) | ~23 hr | 13.7% |
| Ads (interstitial, estimated) | ~3 hr 50 min | 2.3% |
| **Total** | **168 hr** | **100%** |
