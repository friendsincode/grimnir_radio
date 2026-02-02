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

**Goal:** Version history, mobile preview, SEO, performance optimization

---

#### 9F.1: Version History and Rollback

**Data Model:**
```sql
-- Already defined, but expanded:
CREATE TABLE landing_page_versions (
  id UUID PRIMARY KEY,
  landing_page_id UUID NOT NULL REFERENCES landing_pages(id),
  version_number INT NOT NULL,
  config JSONB NOT NULL,
  config_hash VARCHAR(64) NOT NULL,  -- SHA256 for dedup
  change_type VARCHAR(32) NOT NULL,  -- 'publish', 'auto_save', 'restore'
  change_summary TEXT,               -- auto-generated or user-provided
  thumbnail_path VARCHAR(512),       -- screenshot of this version
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMP NOT NULL,

  UNIQUE(landing_page_id, version_number)
);
CREATE INDEX idx_lpv_landing_page ON landing_page_versions(landing_page_id, created_at DESC);
```

**Version Creation Rules:**
- New version on every **publish** (always)
- New version on **auto-save** only if config changed (compare hash)
- New version on **restore** (creates new version from old config)
- Keep last 50 versions per station (configurable)
- Versions older than 90 days auto-pruned (except published versions)

**API Endpoints:**
```
GET  /api/v1/landing-page/versions                    # List versions (paginated)
     ?limit=20&offset=0

GET  /api/v1/landing-page/versions/{id}               # Get version details
     Response: {version_number, config, change_type, created_by, created_at}

GET  /api/v1/landing-page/versions/{id}/preview       # Get preview URL for version
     Response: {preview_url: "/preview/landing-page?version=...&token=..."}

POST /api/v1/landing-page/versions/{id}/restore       # Restore this version
     Response: {restored: true, new_version_number: N}

GET  /api/v1/landing-page/versions/diff?from={id}&to={id}  # Diff two versions
     Response: {changes: [{path: "hero.height", from: "large", to: "medium"}, ...]}
```

**UI Components:**

**Version History Panel:**
```
┌─────────────────────────────────────────────────┐
│  Version History                          [×]   │
├─────────────────────────────────────────────────┤
│                                                 │
│  ┌─────────────────────────────────────────┐   │
│  │ v12 • Published • 2 hours ago           │   │
│  │ By: admin@station.com                   │   │
│  │ "Updated hero background"               │   │
│  │ [Preview] [Restore] [Compare]           │   │
│  └─────────────────────────────────────────┘   │
│                                                 │
│  ┌─────────────────────────────────────────┐   │
│  │ v11 • Auto-save • 3 hours ago           │   │
│  │ By: admin@station.com                   │   │
│  │ [Preview] [Restore] [Compare]           │   │
│  └─────────────────────────────────────────┘   │
│                                                 │
│  ┌─────────────────────────────────────────┐   │
│  │ v10 • Published • Yesterday      [LIVE] │   │
│  │ By: manager@station.com                 │   │
│  │ "Launched new design"                   │   │
│  │ [Preview] [Compare]                     │   │
│  └─────────────────────────────────────────┘   │
│                                                 │
│  [Load More...]                                 │
│                                                 │
└─────────────────────────────────────────────────┘
```

**Diff Viewer:**
```
┌─────────────────────────────────────────────────────────────┐
│  Compare: v10 → v12                                   [×]   │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Changes (5):                                               │
│                                                             │
│  ● hero.backgroundImage                                     │
│    - asset://hero-old-123                                   │
│    + asset://hero-new-456                                   │
│                                                             │
│  ● hero.height                                              │
│    - "medium"                                               │
│    + "large"                                                │
│                                                             │
│  ● content.widgets[2].config.title                          │
│    - "About Us"                                             │
│    + "Our Story"                                            │
│                                                             │
│  ● colors.primary                                           │
│    - "#3B82F6"                                              │
│    + "#10B981"                                              │
│                                                             │
│  ● footer.copyrightText                                     │
│    - "© 2025 Station"                                       │
│    + "© 2026 Station"                                       │
│                                                             │
│  [Side-by-Side Preview]                                     │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Implementation Tasks:**
- [ ] Version creation on publish
- [ ] Auto-save with hash comparison
- [ ] Version list API with pagination
- [ ] Restore version API
- [ ] JSON diff algorithm
- [ ] Version history UI panel
- [ ] Diff viewer UI
- [ ] Side-by-side preview
- [ ] Thumbnail generation (optional - use Playwright/Puppeteer)
- [ ] Version pruning background job

---

#### 9F.2: Mobile/Tablet Preview Modes

**Viewport Presets:**
| Device | Width | Height | Scale |
|--------|-------|--------|-------|
| Desktop | 1440px | 900px | 100% |
| Laptop | 1280px | 800px | 100% |
| Tablet Landscape | 1024px | 768px | 100% |
| Tablet Portrait | 768px | 1024px | 100% |
| Mobile Large | 428px | 926px | 100% |
| Mobile Medium | 390px | 844px | 100% |
| Mobile Small | 375px | 667px | 100% |

**Preview Frame UI:**
```
┌─────────────────────────────────────────────────────────────────┐
│  [Desktop] [Laptop] [Tablet ▼] [Mobile ▼]    [↻ Rotate] [100%▼] │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│         ┌─────────────────────────────┐                         │
│         │ ┌─────────────────────────┐ │                         │
│         │ │                         │ │  ← Device frame         │
│         │ │                         │ │                         │
│         │ │      Preview iframe     │ │                         │
│         │ │                         │ │                         │
│         │ │                         │ │                         │
│         │ │                         │ │                         │
│         │ │                         │ │                         │
│         │ └─────────────────────────┘ │                         │
│         │          ○                  │  ← Home button (visual) │
│         └─────────────────────────────┘                         │
│                                                                  │
│                    390 × 844 • Mobile Medium                     │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Responsive Design System:**
```css
/* Widget container breakpoints */
.widget-container {
  --columns: 3;
}

@media (max-width: 1024px) {
  .widget-container {
    --columns: 2;
  }
}

@media (max-width: 768px) {
  .widget-container {
    --columns: 1;
  }
}

/* Hero section responsive */
.hero--full { height: 100vh; }
.hero--large { height: 70vh; }
.hero--medium { height: 50vh; }
.hero--small { height: 30vh; }

@media (max-width: 768px) {
  .hero--full { height: 80vh; }
  .hero--large { height: 60vh; }
  .hero--medium { height: 50vh; }
  .hero--small { height: 40vh; }
}

/* Player widget responsive */
.player--expanded {
  /* Desktop: horizontal layout */
}

@media (max-width: 768px) {
  .player--expanded {
    /* Mobile: vertical/stacked layout */
  }
}
```

**Mobile-Specific Configuration:**
```json
{
  "responsive": {
    "mobile": {
      "hero": {
        "height": "medium",
        "showPlayer": true,
        "playerVariant": "minimal"
      },
      "header": {
        "logoSize": "small",
        "showTagline": false
      },
      "content": {
        "widgetOrder": ["widget-1", "widget-3", "widget-2"],
        "hiddenWidgets": ["widget-4"]
      }
    },
    "tablet": {
      "content": {
        "columns": 2
      }
    }
  }
}
```

**Implementation Tasks:**
- [ ] Viewport selector UI
- [ ] Preview iframe resizing
- [ ] Device frame overlays (optional visual chrome)
- [ ] Rotate button (portrait/landscape)
- [ ] Zoom control for small viewports
- [ ] Responsive CSS for all widgets
- [ ] Mobile-specific config overrides
- [ ] Widget reordering per breakpoint
- [ ] Widget hide/show per breakpoint
- [ ] Touch interaction testing mode

---

#### 9F.3: SEO Configuration

**SEO Settings in Config:**
```json
{
  "seo": {
    "title": "WXYZ Radio - Your Community Voice",
    "titleTemplate": "%s | WXYZ Radio",
    "description": "Listen live to WXYZ Radio, serving the community since 1985. Music, news, and local voices 24/7.",
    "keywords": ["radio", "community radio", "local music", "WXYZ"],

    "openGraph": {
      "type": "website",
      "image": "asset://og-image-123",
      "imageWidth": 1200,
      "imageHeight": 630,
      "siteName": "WXYZ Radio"
    },

    "twitter": {
      "card": "summary_large_image",
      "site": "@wxyzradio",
      "image": "asset://twitter-card-123"
    },

    "favicon": {
      "ico": "asset://favicon-ico-123",
      "png32": "asset://favicon-32-123",
      "png16": "asset://favicon-16-123",
      "appleTouchIcon": "asset://apple-touch-123"
    },

    "structuredData": {
      "enabled": true,
      "type": "RadioStation",
      "customData": {}
    },

    "robots": {
      "index": true,
      "follow": true,
      "noarchive": false
    },

    "canonical": "https://wxyzradio.com"
  }
}
```

**Generated HTML Head:**
```html
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">

  <!-- Basic SEO -->
  <title>WXYZ Radio - Your Community Voice</title>
  <meta name="description" content="Listen live to WXYZ Radio...">
  <meta name="keywords" content="radio, community radio, local music, WXYZ">
  <link rel="canonical" href="https://wxyzradio.com">
  <meta name="robots" content="index, follow">

  <!-- Open Graph -->
  <meta property="og:type" content="website">
  <meta property="og:title" content="WXYZ Radio - Your Community Voice">
  <meta property="og:description" content="Listen live to WXYZ Radio...">
  <meta property="og:image" content="https://cdn.../og-image.jpg">
  <meta property="og:image:width" content="1200">
  <meta property="og:image:height" content="630">
  <meta property="og:site_name" content="WXYZ Radio">
  <meta property="og:url" content="https://wxyzradio.com">

  <!-- Twitter Card -->
  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:site" content="@wxyzradio">
  <meta name="twitter:title" content="WXYZ Radio - Your Community Voice">
  <meta name="twitter:description" content="Listen live to WXYZ Radio...">
  <meta name="twitter:image" content="https://cdn.../twitter-card.jpg">

  <!-- Favicons -->
  <link rel="icon" type="image/x-icon" href="/favicon.ico">
  <link rel="icon" type="image/png" sizes="32x32" href="/favicon-32x32.png">
  <link rel="icon" type="image/png" sizes="16x16" href="/favicon-16x16.png">
  <link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon.png">

  <!-- Structured Data -->
  <script type="application/ld+json">
  {
    "@context": "https://schema.org",
    "@type": "RadioStation",
    "name": "WXYZ Radio",
    "description": "Listen live to WXYZ Radio...",
    "url": "https://wxyzradio.com",
    "logo": "https://cdn.../logo.png",
    "sameAs": [
      "https://twitter.com/wxyzradio",
      "https://facebook.com/wxyzradio"
    ]
  }
  </script>
</head>
```

**SEO Editor Panel:**
```
┌─────────────────────────────────────────────────────────────┐
│  SEO Settings                                               │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Page Title                                                 │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ WXYZ Radio - Your Community Voice                   │   │
│  └─────────────────────────────────────────────────────┘   │
│  56/60 characters                                           │
│                                                             │
│  Meta Description                                           │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ Listen live to WXYZ Radio, serving the community    │   │
│  │ since 1985. Music, news, and local voices 24/7.     │   │
│  └─────────────────────────────────────────────────────┘   │
│  142/160 characters                                         │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Social Preview                                             │
│                                                             │
│  ┌──────────────────────────────────────┐                  │
│  │ Google                               │                  │
│  │ ┌────────────────────────────────┐   │                  │
│  │ │ WXYZ Radio - Your Community... │   │                  │
│  │ │ wxyzradio.com                  │   │                  │
│  │ │ Listen live to WXYZ Radio...   │   │                  │
│  │ └────────────────────────────────┘   │                  │
│  └──────────────────────────────────────┘                  │
│                                                             │
│  ┌──────────────────────────────────────┐                  │
│  │ Facebook / Twitter                   │                  │
│  │ ┌────────────────────────────────┐   │                  │
│  │ │ [OG Image Preview            ] │   │                  │
│  │ │ WXYZ Radio - Your Community... │   │                  │
│  │ │ Listen live to WXYZ Radio...   │   │                  │
│  │ └────────────────────────────────┘   │                  │
│  └──────────────────────────────────────┘                  │
│                                                             │
│  [Upload OG Image] (1200×630 recommended)                   │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Favicon                                                    │
│  ┌────┐                                                    │
│  │ 🎵 │  [Upload New Favicon]                              │
│  └────┘                                                    │
│  Auto-generates all sizes from uploaded image               │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Advanced                                                   │
│  ☑ Allow search engine indexing                            │
│  ☑ Allow search engines to follow links                    │
│  ☐ Prevent archiving                                       │
│                                                             │
│  Canonical URL (optional)                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ https://wxyzradio.com                               │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Implementation Tasks:**
- [ ] SEO config schema
- [ ] Meta tag renderer
- [ ] Open Graph tag renderer
- [ ] Twitter Card tag renderer
- [ ] Structured data (JSON-LD) generator
- [ ] Favicon auto-generation (from single upload)
- [ ] SEO editor panel UI
- [ ] Google preview mockup
- [ ] Social card preview mockup
- [ ] Character count indicators
- [ ] Robots meta tag control

---

#### 9F.4: Custom CSS Support

**CSS Configuration:**
```json
{
  "customization": {
    "css": {
      "enabled": true,
      "code": ".hero { border-radius: 12px; }\n.player-widget { box-shadow: 0 4px 6px rgba(0,0,0,0.1); }",
      "validated": true,
      "lastValidated": "2026-01-15T10:30:00Z"
    }
  }
}
```

**CSS Editor Panel:**
```
┌─────────────────────────────────────────────────────────────┐
│  Custom CSS                                    [?] Help     │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ⚠️  Custom CSS is for advanced users. Invalid CSS may      │
│     break your page layout.                                 │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ 1  /* Custom styles for WXYZ Radio */               │   │
│  │ 2                                                   │   │
│  │ 3  .hero {                                          │   │
│  │ 4    border-radius: 12px;                           │   │
│  │ 5    overflow: hidden;                              │   │
│  │ 6  }                                                │   │
│  │ 7                                                   │   │
│  │ 8  .player-widget {                                 │   │
│  │ 9    box-shadow: 0 4px 6px rgba(0,0,0,0.1);        │   │
│  │10  }                                                │   │
│  │11                                                   │   │
│  │12  .widget-title {                                  │   │
│  │13    font-weight: 700;                              │   │
│  │14    text-transform: uppercase;                     │   │
│  │15    letter-spacing: 0.05em;                        │   │
│  │16  }                                                │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ✅ CSS is valid                          [Apply to Preview]│
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  CSS Class Reference:                                       │
│                                                             │
│  Layout:                                                    │
│  • .landing-page - Page container                          │
│  • .header - Header section                                │
│  • .hero - Hero section                                    │
│  • .content-area - Main content grid                       │
│  • .footer - Footer section                                │
│                                                             │
│  Widgets:                                                   │
│  • .widget - Any widget container                          │
│  • .widget-title - Widget heading                          │
│  • .player-widget - Player widget                          │
│  • .schedule-widget - Schedule widget                      │
│  • .recent-tracks-widget - Recent tracks                   │
│  • .text-block - Text content block                        │
│                                                             │
│  [View Full Reference →]                                    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**CSS Validation:**
- Parse CSS server-side before saving
- Block dangerous properties: `position: fixed`, `z-index > 9999`, `pointer-events: none` on body
- Block `@import` (security)
- Warn on `!important` overuse
- Scope all CSS to `.landing-page` container (prevent editor breakage)

**Implementation Tasks:**
- [ ] CSS editor with syntax highlighting (CodeMirror/Monaco)
- [ ] Server-side CSS validation
- [ ] Dangerous property blocking
- [ ] Auto-scoping CSS to landing page container
- [ ] CSS class reference documentation
- [ ] Live preview of CSS changes
- [ ] CSS minification for production

---

#### 9F.5: Custom Head Content (Analytics & Scripts)

**Head Content Configuration:**
```json
{
  "customization": {
    "headContent": {
      "enabled": true,
      "scripts": [
        {
          "id": "google-analytics",
          "name": "Google Analytics",
          "type": "analytics",
          "code": "<!-- Google tag (gtag.js) -->\n<script async src=\"https://www.googletagmanager.com/gtag/js?id=G-XXXXXXX\"></script>\n<script>\n  window.dataLayer = window.dataLayer || [];\n  function gtag(){dataLayer.push(arguments);}\n  gtag('js', new Date());\n  gtag('config', 'G-XXXXXXX');\n</script>",
          "position": "head",
          "enabled": true
        },
        {
          "id": "facebook-pixel",
          "name": "Facebook Pixel",
          "type": "analytics",
          "code": "<!-- Facebook Pixel Code -->...",
          "position": "head",
          "enabled": true
        },
        {
          "id": "custom-chat",
          "name": "Live Chat Widget",
          "type": "widget",
          "code": "<script src=\"https://chat.example.com/widget.js\"></script>",
          "position": "body_end",
          "enabled": false
        }
      ]
    }
  }
}
```

**Script Positions:**
- `head` - Inside `<head>` tag
- `body_start` - After opening `<body>`
- `body_end` - Before closing `</body>`

**Preset Integrations:**
| Service | Type | Setup |
|---------|------|-------|
| Google Analytics 4 | Analytics | Enter Measurement ID (G-XXXXX) |
| Google Tag Manager | Tag Manager | Enter Container ID (GTM-XXXXX) |
| Facebook Pixel | Analytics | Enter Pixel ID |
| Plausible | Analytics | Enter Domain |
| Fathom | Analytics | Enter Site ID |
| Hotjar | Heatmaps | Enter Site ID |
| Crisp Chat | Chat | Enter Website ID |
| Tawk.to | Chat | Enter Property ID |

**Custom Scripts Editor:**
```
┌─────────────────────────────────────────────────────────────┐
│  Scripts & Analytics                                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Quick Add Integration:                                     │
│  [Google Analytics ▼] [Add]                                 │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Active Scripts:                                            │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ☑ Google Analytics 4                    [Edit] [×]  │   │
│  │   Position: <head>                                  │   │
│  │   ID: G-ABC123XYZ                                   │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ☐ Facebook Pixel (disabled)             [Edit] [×]  │   │
│  │   Position: <head>                                  │   │
│  │   ID: 1234567890                                    │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Custom Script:                                             │
│                                                             │
│  Name: _______________                                      │
│  Position: [<head> ▼]                                       │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ <script>                                            │   │
│  │   // Your custom script here                        │   │
│  │ </script>                                           │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  [Add Script]                                               │
│                                                             │
│  ⚠️  Only add scripts from trusted sources. Malicious      │
│     scripts can compromise your site and visitors.          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Security Considerations:**
- Only admin role can add custom scripts
- Log all script changes to audit log
- Option to require approval for script changes
- CSP headers to restrict script sources (if strict mode enabled)
- Script content stored encrypted at rest

**Implementation Tasks:**
- [ ] Script configuration schema
- [ ] Preset integration templates
- [ ] Script position injection in renderer
- [ ] Custom script editor UI
- [ ] Enable/disable toggle per script
- [ ] Admin-only access control
- [ ] Audit logging for script changes
- [ ] Script validation (basic HTML parsing)

---

#### 9F.6: Page Loading Optimization

**Performance Targets:**
- First Contentful Paint (FCP): < 1.5s
- Largest Contentful Paint (LCP): < 2.5s
- Cumulative Layout Shift (CLS): < 0.1
- Time to Interactive (TTI): < 3.5s
- Google PageSpeed Score: > 80

**Optimization Strategies:**

**1. Image Optimization:**
```go
// On upload, generate multiple sizes
type ImageVariants struct {
    Original   string // original upload
    Large      string // 1920px max width
    Medium     string // 1280px max width
    Small      string // 640px max width
    Thumbnail  string // 320px max width
    WebP       bool   // generate WebP versions of all
    AVIF       bool   // generate AVIF versions (if supported)
}
```

**Responsive Images in HTML:**
```html
<picture>
  <source
    type="image/avif"
    srcset="/assets/hero-small.avif 640w,
            /assets/hero-medium.avif 1280w,
            /assets/hero-large.avif 1920w"
    sizes="100vw">
  <source
    type="image/webp"
    srcset="/assets/hero-small.webp 640w,
            /assets/hero-medium.webp 1280w,
            /assets/hero-large.webp 1920w"
    sizes="100vw">
  <img
    src="/assets/hero-medium.jpg"
    srcset="/assets/hero-small.jpg 640w,
            /assets/hero-medium.jpg 1280w,
            /assets/hero-large.jpg 1920w"
    sizes="100vw"
    alt="Hero background"
    loading="lazy"
    decoding="async">
</picture>
```

**2. CSS/JS Optimization:**
- Inline critical CSS in `<head>`
- Defer non-critical CSS
- Async load JavaScript
- Bundle and minify all assets
- Tree-shake unused CSS

**3. Lazy Loading:**
```html
<!-- Below-fold widgets -->
<div class="widget" data-lazy="true">
  <noscript>
    <!-- Full content for no-JS -->
  </noscript>
  <!-- Placeholder skeleton -->
  <div class="widget-skeleton"></div>
</div>

<script>
// Intersection Observer to load widgets when visible
</script>
```

**4. Caching Strategy:**
```
# Static assets (images, CSS, JS)
Cache-Control: public, max-age=31536000, immutable

# HTML pages
Cache-Control: public, max-age=300, stale-while-revalidate=86400

# API responses (now playing, schedule)
Cache-Control: public, max-age=10, stale-while-revalidate=30
```

**5. Preloading:**
```html
<!-- Preload critical assets -->
<link rel="preload" href="/fonts/inter-var.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="/css/critical.css" as="style">
<link rel="preconnect" href="https://cdn.example.com">
<link rel="dns-prefetch" href="https://stream.example.com">
```

**6. Server-Side Rendering:**
- Render full HTML on server (no client-side hydration for static content)
- Stream HTML response where possible
- Edge caching with CDN

**Performance Dashboard:**
```
┌─────────────────────────────────────────────────────────────┐
│  Performance                                                │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  PageSpeed Score                                            │
│                                                             │
│  Desktop: ████████████████████░░░░ 85                       │
│  Mobile:  ██████████████████░░░░░░ 78                       │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Core Web Vitals                                            │
│                                                             │
│  LCP (Largest Contentful Paint)                             │
│  ●  2.1s  [Good: < 2.5s]                                    │
│                                                             │
│  FID (First Input Delay)                                    │
│  ●  45ms  [Good: < 100ms]                                   │
│                                                             │
│  CLS (Cumulative Layout Shift)                              │
│  ●  0.05  [Good: < 0.1]                                     │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Recommendations:                                           │
│                                                             │
│  ⚠️  Hero image could be smaller (2.4MB → optimize)         │
│  ⚠️  3 render-blocking resources detected                   │
│  ✅  Text compression enabled                               │
│  ✅  Browser caching configured                             │
│                                                             │
│  [Run Full Audit]  [View Details]                           │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Implementation Tasks:**
- [ ] Image processing pipeline (resize, WebP, AVIF)
- [ ] Responsive image HTML generation
- [ ] Critical CSS extraction
- [ ] CSS/JS bundling and minification
- [ ] Lazy loading for below-fold widgets
- [ ] Cache headers configuration
- [ ] Preload/preconnect hints
- [ ] Performance monitoring integration
- [ ] PageSpeed API integration (optional)
- [ ] Performance dashboard UI
- [ ] Optimization recommendations engine

---

#### 9F Implementation Summary

| Task | Priority | Complexity |
|------|----------|------------|
| Version history | High | Medium |
| Version rollback | High | Low |
| Diff viewer | Medium | Medium |
| Mobile preview | High | Low |
| Viewport presets | High | Low |
| Responsive config | Medium | Medium |
| SEO meta tags | High | Low |
| OG/Twitter cards | High | Low |
| Structured data | Medium | Low |
| Favicon generator | Low | Medium |
| SEO editor UI | High | Medium |
| Custom CSS editor | Medium | Medium |
| CSS validation | Medium | Medium |
| Custom scripts | Medium | Medium |
| Script presets | Low | Low |
| Image optimization | High | High |
| Lazy loading | Medium | Medium |
| Cache headers | High | Low |
| Performance dashboard | Low | Medium |

**Recommended Order:**
1. Mobile preview (quick win, high value)
2. SEO configuration (essential for launch)
3. Version history + rollback (safety net)
4. Image optimization (performance)
5. Custom CSS
6. Custom scripts
7. Performance dashboard
8. Diff viewer (nice to have)

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

---

## White-Labeling Support

**Goal:** Allow complete removal/replacement of "Grimnir Radio" branding so operators can present the platform as their own.

### Platform Branding Configuration

**System-wide settings (admin only):**
```json
{
  "platform": {
    "name": "MyRadio Platform",
    "tagline": "Professional Radio Automation",
    "logo": "asset://platform-logo-123",
    "logoMark": "asset://platform-mark-123",
    "favicon": "asset://platform-favicon-123",
    "supportEmail": "support@myradio.com",
    "supportUrl": "https://myradio.com/support",
    "documentationUrl": "https://docs.myradio.com",
    "copyrightHolder": "MyRadio Inc.",
    "hideGrimnirBranding": true
  }
}
```

### Affected Areas

| Area | Default | White-labeled |
|------|---------|---------------|
| Login page title | "Grimnir Radio" | Custom platform name |
| Login page logo | Grimnir logo | Custom logo |
| Dashboard header | "Grimnir Radio" | Custom name |
| Dashboard favicon | Grimnir icon | Custom favicon |
| Email sender name | "Grimnir Radio" | Custom name |
| Email footer | "Powered by Grimnir Radio" | Hidden or custom |
| API docs | "Grimnir Radio API" | Custom name |
| Error pages | Grimnir branding | Custom branding |
| "About" links | grimnir_radio repo | Custom or hidden |

### Implementation

**Environment Variables:**
```bash
GRIMNIR_PLATFORM_NAME="MyRadio Platform"
GRIMNIR_PLATFORM_LOGO_URL="/assets/custom-logo.png"
GRIMNIR_HIDE_GRIMNIR_BRANDING=true
GRIMNIR_SUPPORT_EMAIL="support@myradio.com"
```

**Template Variables:**
```go
type PlatformBranding struct {
    Name            string
    Tagline         string
    LogoURL         string
    LogoMarkURL     string
    FaviconURL      string
    SupportEmail    string
    SupportURL      string
    DocsURL         string
    CopyrightHolder string
    ShowPoweredBy   bool  // "Powered by Grimnir Radio" in footer
}

// Available in all templates as .Platform
```

**Database Table:**
```sql
CREATE TABLE platform_settings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  key VARCHAR(64) NOT NULL UNIQUE,
  value JSONB NOT NULL,
  updated_by UUID REFERENCES users(id),
  updated_at TIMESTAMP NOT NULL DEFAULT now()
);
```

### White-Label Admin UI

```
┌─────────────────────────────────────────────────────────────┐
│  Platform Branding (Admin Only)                             │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Platform Name                                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ MyRadio Platform                                    │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  Tagline                                                    │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ Professional Radio Automation                       │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  Logo (displayed in header)                                 │
│  ┌────────────────┐                                        │
│  │  [MyRadio]     │  [Upload New]                          │
│  └────────────────┘                                        │
│                                                             │
│  Logo Mark (square, for favicon/mobile)                     │
│  ┌────┐                                                    │
│  │ M  │  [Upload New]                                      │
│  └────┘                                                    │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Support & Links                                            │
│                                                             │
│  Support Email: support@myradio.com                         │
│  Support URL:   https://myradio.com/support                 │
│  Docs URL:      https://docs.myradio.com                    │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Footer                                                     │
│                                                             │
│  Copyright Holder: MyRadio Inc.                             │
│                                                             │
│  ☐ Show "Powered by Grimnir Radio" in footer               │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  [Save Changes]                                             │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Implementation Tasks

- [ ] Platform branding database table
- [ ] Environment variable fallbacks
- [ ] Template variable injection
- [ ] Login page branding
- [ ] Dashboard header branding
- [ ] Email template branding
- [ ] API documentation branding
- [ ] Error page branding
- [ ] Admin UI for branding settings
- [ ] Asset upload for logos/favicon

---

## Future Enhancements

- **Multiple pages** (About, Contact, Schedule as separate pages)
- **A/B testing** (test different layouts)
- **Analytics dashboard** (which widgets get clicks)
- **Template marketplace** (share/download configurations)
- **Custom domains** (station.com instead of grimnir/station)
- **PWA support** (installable web app)
