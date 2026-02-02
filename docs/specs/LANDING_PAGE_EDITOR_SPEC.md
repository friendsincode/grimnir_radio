# Landing Page Editor Specification

**Status:** Planning
**Priority:** Phase 9 (after Advanced Scheduling)

## Overview

A visual editor that lets station operators customize their public-facing landing page without writing code. Stations can add their branding, choose which widgets to display, arrange the layout, and publish changes instantly.

---

## Goals

1. **No coding required** - Station managers can customize everything visually
2. **Professional results** - Built-in themes and widgets look polished out of the box
3. **Flexible layouts** - Drag-and-drop arrangement of content blocks
4. **Mobile responsive** - All layouts work on desktop, tablet, and mobile
5. **Fast loading** - Server-rendered for SEO and performance
6. **Live preview** - See changes before publishing

---

## User Stories

**As a station manager, I want to:**
- Upload my station logo and set brand colors
- Choose which widgets appear on my landing page
- Arrange widgets in the order I want
- Add custom text blocks (about us, contact info)
- Preview changes before they go live
- Revert to a previous version if I make a mistake

**As a listener, I want to:**
- See what's currently playing
- Easily find the listen/play button
- View the upcoming schedule
- Learn about the station and DJs
- Find social media links

---

## Page Structure

```
┌─────────────────────────────────────────────────────────────┐
│                        HEADER                                │
│   [Logo]     Station Name / Tagline        [Social Icons]   │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│                        HERO SECTION                          │
│            (Background image/video + overlay)                │
│                                                              │
│                    ┌─────────────────┐                       │
│                    │  PLAYER WIDGET  │                       │
│                    │   Now Playing   │                       │
│                    │   [▶ Listen]    │                       │
│                    └─────────────────┘                       │
│                                                              │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│                      CONTENT AREA                            │
│         (Configurable grid of widgets/blocks)                │
│                                                              │
│   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│   │   Schedule   │  │    Recent    │  │   About Us   │      │
│   │   Widget     │  │    Tracks    │  │    Block     │      │
│   └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                              │
│   ┌──────────────┐  ┌──────────────┐                        │
│   │     DJs      │  │   Contact    │                        │
│   │   Widget     │  │    Block     │                        │
│   └──────────────┘  └──────────────┘                        │
│                                                              │
├─────────────────────────────────────────────────────────────┤
│                        FOOTER                                │
│        [Links]    [Copyright]    [Social Icons]              │
└─────────────────────────────────────────────────────────────┘
```

---

## Available Widgets

### Core Widgets

**1. Player Widget**
- Now playing (title, artist, artwork)
- Play/pause button (connects to stream)
- Volume control
- Stream selector (if multiple mounts)
- Variants: minimal, standard, expanded

**2. Schedule Widget**
- Today's schedule
- Current show highlighted
- Configurable: show X upcoming shows
- Link to full schedule page
- Variants: list, timeline, compact

**3. Recent Tracks Widget**
- Last N played tracks
- Artwork, title, artist
- Timestamps
- Configurable count (5, 10, 15, 20)

**4. Upcoming Shows Widget**
- Next N scheduled shows
- Show name, host, time
- Show artwork if available
- Configurable count

**5. DJ/Host Widget**
- Grid or list of DJs
- Photo, name, bio snippet
- Currently on-air indicator
- Link to full profile
- Variants: grid, carousel, list

**6. Social Feed Widget**
- Embedded social media feed
- Twitter/X timeline
- Facebook page feed
- Instagram grid
- Configurable source

### Content Blocks

**7. Text Block**
- Rich text content (WYSIWYG editor)
- Headings, paragraphs, lists, links
- Images inline
- Use for: About, History, Contact, FAQ

**8. Image Block**
- Single image with optional caption
- Link on click
- Alignment options

**9. Image Gallery**
- Grid of images
- Lightbox on click
- Captions

**10. Video Block**
- Embedded video (YouTube, Vimeo, self-hosted)
- Autoplay option (muted)
- Use for: Station promo, music videos

**11. Call-to-Action Block**
- Headline + subtext + button
- Configurable button link
- Use for: Donate, Subscribe, Contact

**12. Contact Block**
- Contact form
- Email, phone, address display
- Map embed (optional)

**13. Newsletter Signup**
- Email capture form
- Integration with email services (Mailchimp, etc.)

**14. Custom HTML Block**
- Raw HTML/embed code
- For advanced users
- Sandboxed for security

**15. Spacer Block**
- Vertical spacing
- Configurable height

**16. Divider Block**
- Horizontal line
- Style options (solid, dashed, gradient)

---

## Theme System

### Built-in Themes

| Theme | Description |
|-------|-------------|
| **Default** | Clean, professional, neutral colors |
| **Dark** | Dark background, light text, modern feel |
| **Light** | Bright, airy, minimal |
| **Bold** | Strong colors, high contrast |
| **Vintage** | Warm tones, retro radio aesthetic |
| **Neon** | Dark with neon accents, club/electronic feel |
| **Community** | Friendly, approachable, warm colors |

### Customizable Properties

**Colors:**
- Primary color (buttons, links, accents)
- Secondary color (highlights)
- Background color
- Text color
- Header background
- Footer background

**Typography:**
- Heading font (from Google Fonts selection)
- Body font (from Google Fonts selection)
- Base font size

**Header:**
- Logo (upload)
- Logo position (left, center)
- Show/hide station name
- Show/hide tagline
- Header style (transparent, solid, gradient)

**Hero Section:**
- Background type (color, image, video)
- Background image (upload)
- Background video (URL)
- Overlay color + opacity
- Height (small, medium, large, full screen)
- Content alignment

**Footer:**
- Background color
- Content (copyright, links, social)
- Show/hide elements

---

## Data Model

```sql
-- Landing page configuration
CREATE TABLE landing_pages (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id) UNIQUE,
  published_config JSONB,      -- currently live configuration
  draft_config JSONB,          -- work-in-progress changes
  theme VARCHAR(64) NOT NULL DEFAULT 'default',
  custom_css TEXT,             -- advanced: custom CSS overrides
  custom_head TEXT,            -- advanced: custom <head> content (analytics, etc.)
  published_at TIMESTAMP,
  updated_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL
);

-- Landing page assets (uploaded images, etc.)
CREATE TABLE landing_page_assets (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  asset_type VARCHAR(32) NOT NULL,  -- 'logo', 'background', 'image', 'favicon'
  file_path VARCHAR(512) NOT NULL,
  file_name VARCHAR(255) NOT NULL,
  mime_type VARCHAR(64) NOT NULL,
  file_size INT NOT NULL,
  dimensions JSONB,  -- {width, height} for images
  uploaded_by UUID REFERENCES users(id),
  created_at TIMESTAMP NOT NULL
);

-- Landing page versions (for history/rollback)
CREATE TABLE landing_page_versions (
  id UUID PRIMARY KEY,
  landing_page_id UUID NOT NULL REFERENCES landing_pages(id),
  version_number INT NOT NULL,
  config JSONB NOT NULL,
  change_summary TEXT,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMP NOT NULL
);
```

### Configuration JSON Structure

```json
{
  "version": 1,
  "theme": "default",
  "colors": {
    "primary": "#3B82F6",
    "secondary": "#10B981",
    "background": "#FFFFFF",
    "text": "#1F2937",
    "headerBg": "#1F2937",
    "footerBg": "#1F2937"
  },
  "typography": {
    "headingFont": "Inter",
    "bodyFont": "Inter",
    "baseFontSize": 16
  },
  "header": {
    "logo": "asset://logo-12345",
    "logoPosition": "left",
    "showStationName": true,
    "showTagline": true,
    "tagline": "Your Community Radio",
    "style": "solid",
    "socialLinks": [
      {"platform": "twitter", "url": "https://twitter.com/..."},
      {"platform": "facebook", "url": "https://facebook.com/..."},
      {"platform": "instagram", "url": "https://instagram.com/..."}
    ]
  },
  "hero": {
    "enabled": true,
    "backgroundType": "image",
    "backgroundImage": "asset://hero-bg-12345",
    "overlayColor": "#000000",
    "overlayOpacity": 0.5,
    "height": "large",
    "showPlayer": true,
    "playerVariant": "expanded"
  },
  "content": {
    "layout": "grid",
    "columns": 3,
    "gap": "medium",
    "widgets": [
      {
        "id": "widget-1",
        "type": "schedule",
        "config": {
          "title": "Today's Schedule",
          "showCount": 5,
          "variant": "list"
        },
        "position": {"column": 1, "row": 1, "width": 1}
      },
      {
        "id": "widget-2",
        "type": "recent-tracks",
        "config": {
          "title": "Recently Played",
          "count": 10,
          "showArtwork": true
        },
        "position": {"column": 2, "row": 1, "width": 1}
      },
      {
        "id": "widget-3",
        "type": "text",
        "config": {
          "title": "About Us",
          "content": "<p>Welcome to our station...</p>"
        },
        "position": {"column": 3, "row": 1, "width": 1}
      }
    ]
  },
  "footer": {
    "showCopyright": true,
    "copyrightText": "© 2026 Station Name",
    "links": [
      {"label": "Contact", "url": "/contact"},
      {"label": "Privacy", "url": "/privacy"}
    ],
    "showSocialLinks": true
  },
  "seo": {
    "title": "Station Name - Your Community Radio",
    "description": "Listen to the best music...",
    "ogImage": "asset://og-image-12345"
  }
}
```

---

## API Endpoints

```
# Landing Page Configuration
GET    /api/v1/landing-page                    # Get current config (draft + published)
PUT    /api/v1/landing-page                    # Update draft config
POST   /api/v1/landing-page/publish            # Publish draft to live
POST   /api/v1/landing-page/discard-draft      # Discard draft changes
POST   /api/v1/landing-page/preview            # Generate preview URL

# Assets
POST   /api/v1/landing-page/assets             # Upload asset
GET    /api/v1/landing-page/assets             # List assets
DELETE /api/v1/landing-page/assets/{id}        # Delete asset

# Versions
GET    /api/v1/landing-page/versions           # List versions
GET    /api/v1/landing-page/versions/{id}      # Get version config
POST   /api/v1/landing-page/versions/{id}/restore  # Restore version

# Themes
GET    /api/v1/landing-page/themes             # List available themes
GET    /api/v1/landing-page/themes/{name}      # Get theme defaults
```

---

## Editor Interface

### Layout

```
┌─────────────────────────────────────────────────────────────────────┐
│  [← Back to Dashboard]     Landing Page Editor     [Preview] [Save] │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────────┐  ┌───────────────────────────────────────────────┐│
│  │              │  │                                                ││
│  │   SIDEBAR    │  │              LIVE PREVIEW                      ││
│  │              │  │                                                ││
│  │  [Widgets]   │  │   (Interactive preview of the landing page)   ││
│  │  [Theme]     │  │                                                ││
│  │  [Header]    │  │   Click any element to select and edit        ││
│  │  [Hero]      │  │                                                ││
│  │  [Footer]    │  │                                                ││
│  │  [SEO]       │  │                                                ││
│  │              │  │                                                ││
│  │  ──────────  │  │                                                ││
│  │              │  │                                                ││
│  │  PROPERTIES  │  │                                                ││
│  │              │  │                                                ││
│  │  (Config for │  │                                                ││
│  │   selected   │  │                                                ││
│  │   element)   │  │                                                ││
│  │              │  │                                                ││
│  └──────────────┘  └───────────────────────────────────────────────┘│
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Sidebar Tabs

**Widgets Tab:**
- Draggable widget list
- Drag onto preview to add
- Widget categories (Core, Content, Advanced)

**Theme Tab:**
- Theme selector (thumbnails)
- Color pickers
- Font selectors
- Apply theme button

**Header Tab:**
- Logo upload
- Logo position toggle
- Station name toggle
- Tagline edit
- Social links editor

**Hero Tab:**
- Enable/disable toggle
- Background type selector
- Image/video upload
- Overlay controls
- Height selector
- Player toggle

**Footer Tab:**
- Copyright text
- Links editor
- Social links toggle

**SEO Tab:**
- Page title
- Meta description
- OG image upload
- Preview (Google/social)

### Properties Panel

When a widget is selected:
- Widget-specific configuration
- Delete widget button
- Duplicate widget button
- Move up/down buttons

### Toolbar Actions

| Button | Action |
|--------|--------|
| Preview | Open preview in new tab |
| Save | Save draft (auto-saves too) |
| Publish | Publish draft to live |
| Discard | Discard draft changes |
| History | View version history |
| Desktop/Tablet/Mobile | Switch preview viewport |

---

## Implementation Phases

### Phase 9A: Foundation

**Goal:** Basic landing page with theme support

**Tasks:**
- [ ] Add `landing_pages` table and model
- [ ] Add `landing_page_assets` table and model
- [ ] Create default configuration structure
- [ ] Implement theme system (built-in themes)
- [ ] Server-side page renderer
- [ ] Basic public landing page route

**Deliverable:** Stations get a default landing page that renders

---

### Phase 9B: Core Widgets

**Goal:** Implement essential widgets

**Tasks:**
- [ ] Player widget (now playing, play button)
- [ ] Schedule widget (today's shows)
- [ ] Recent tracks widget
- [ ] Text block (with WYSIWYG editor)
- [ ] Widget registry and renderer

**Deliverable:** Landing page shows live data

---

### Phase 9C: Editor UI

**Goal:** Visual editor for customization

**Tasks:**
- [ ] Editor page layout (sidebar + preview)
- [ ] Live preview iframe
- [ ] Drag-and-drop widget placement
- [ ] Widget selection and configuration
- [ ] Theme customization panel
- [ ] Header/footer configuration
- [ ] Draft/publish workflow
- [ ] Auto-save

**Deliverable:** Station managers can customize their page visually

---

### Phase 9D: Asset Management

**Goal:** Upload and manage images

**Tasks:**
- [ ] Asset upload API
- [ ] Asset library UI
- [ ] Logo upload
- [ ] Background image upload
- [ ] Image optimization (resize, compress)
- [ ] Asset deletion with orphan cleanup

**Deliverable:** Can upload and use custom images

---

### Phase 9E: Additional Widgets

**Goal:** Complete widget library

**Tasks:**
- [ ] DJ/host widget
- [ ] Upcoming shows widget
- [ ] Image block
- [ ] Image gallery
- [ ] Video block
- [ ] Call-to-action block
- [ ] Contact block
- [ ] Social feed widget
- [ ] Newsletter signup
- [ ] Custom HTML block

**Deliverable:** Full widget library available

---

### Phase 9F: Advanced Features

**Goal:** Version history, mobile preview, SEO

**Tasks:**
- [ ] Version history and rollback
- [ ] Mobile/tablet preview modes
- [ ] SEO configuration
- [ ] Custom CSS support
- [ ] Custom head content (analytics)
- [ ] Page loading optimization

**Deliverable:** Production-ready landing page editor

---

## Technical Notes

### Frontend Stack

**Editor:**
- HTMX for interactivity
- Alpine.js for complex UI state
- Sortable.js for drag-and-drop
- TinyMCE or Quill for rich text editing

**Preview:**
- iframe with postMessage communication
- Real-time updates as config changes

### Rendering

**Server-side rendering** for production pages:
- SEO friendly
- Fast initial load
- Go templates with widget components

**Client-side hydration** for interactivity:
- Player widget needs JavaScript
- Minimal JS footprint

### Performance

- Lazy load below-fold widgets
- Image optimization on upload
- CSS/JS bundled and minified
- CDN for assets (if configured)

### Security

- Sanitize all user HTML content
- CSP headers for custom HTML widget
- Validate asset uploads (type, size)
- Rate limit asset uploads

---

## Acceptance Criteria

### Phase 9A
- [ ] Default landing page renders for each station
- [ ] Theme selection works
- [ ] Page loads in < 2 seconds

### Phase 9B
- [ ] Player shows now playing and plays stream
- [ ] Schedule shows today's shows
- [ ] Recent tracks updates in real-time

### Phase 9C
- [ ] Can drag widgets to rearrange
- [ ] Changes preview in real-time
- [ ] Can publish changes
- [ ] Can discard draft

### Phase 9D
- [ ] Can upload logo
- [ ] Can upload background image
- [ ] Assets appear in library

### Phase 9E
- [ ] All widgets render correctly
- [ ] Widgets are configurable

### Phase 9F
- [ ] Can restore previous version
- [ ] Mobile preview accurate
- [ ] SEO meta tags render
- [ ] Google PageSpeed score > 80

---

## Example Configurations

### Minimal (Music-focused)
```json
{
  "hero": {"enabled": true, "showPlayer": true, "height": "full"},
  "content": {"widgets": []}
}
```
Full-screen player only.

### Community Station
```json
{
  "hero": {"enabled": true, "showPlayer": true, "height": "medium"},
  "content": {
    "widgets": [
      {"type": "schedule"},
      {"type": "recent-tracks"},
      {"type": "text", "config": {"title": "About Our Station"}},
      {"type": "dj-grid"},
      {"type": "contact"}
    ]
  }
}
```

### News/Talk Station
```json
{
  "hero": {"enabled": true, "showPlayer": true, "height": "small"},
  "content": {
    "widgets": [
      {"type": "upcoming-shows"},
      {"type": "schedule"},
      {"type": "text", "config": {"title": "Latest News"}},
      {"type": "social-feed"},
      {"type": "newsletter-signup"}
    ]
  }
}
```

---

## Future Enhancements

- **Multiple pages** (About, Contact, Schedule as separate pages)
- **A/B testing** (test different layouts)
- **Analytics dashboard** (which widgets get clicks)
- **Template marketplace** (share/download configurations)
- **Custom domains** (station.com instead of grimnir/station)
- **PWA support** (installable web app)
