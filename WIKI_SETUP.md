# Wiki Documentation Structure

Grimnir Radio now has a comprehensive wiki structure ready for publishing to GitHub's wiki system.

## ✅ What's Been Created

### Wiki Structure (9 complete pages + stubs)

**Complete Documentation:**
- ✅ `wiki/Home.md` - Main landing page with overview
- ✅ `wiki/_Sidebar.md` - Navigation sidebar
- ✅ `wiki/Getting-Started.md` - Quick start guide
- ✅ `wiki/Installation.md` - Installation methods (Docker, Nix, source)
- ✅ `wiki/Architecture.md` - System architecture and design
- ✅ `wiki/Configuration.md` - Environment variables and configuration
- ✅ `wiki/CHANGELOG.md` - Version history (copied from docs)
- ✅ `wiki/Roadmap.md` - Future plans (v1.1.0+)
- ✅ `wiki/README.md` - Wiki maintenance guide

**Publish Script:**
- ✅ `scripts/publish-wiki.sh` - Automated wiki publishing script

### Documentation Cleaned Up

**docs/ Directory Status:**
- Contains original technical specs and reference docs
- Can be kept for development reference
- Wiki extracts/consolidates key user-facing content

## 📋 Publishing the Wiki

### Option 1: Automated Script (Recommended)

```bash
# Publish all wiki pages to GitHub Wiki
./scripts/publish-wiki.sh
```

This script will:
1. Clone your GitHub wiki repository
2. Copy all markdown files from `wiki/` directory
3. Commit changes
4. Push to GitHub

### Option 2: GitHub Web UI

1. Go to https://github.com/friendsincode/grimnir_radio/wiki
2. Click "Create the first page" (if new wiki)
3. Manually copy content from each `wiki/*.md` file
4. Save each page

### Option 3: Manual Git Clone

```bash
# Clone wiki repository
git clone https://github.com/friendsincode/grimnir_radio.wiki.git

# Copy wiki files
cp -r wiki/*.md grimnir_radio.wiki/

# Commit and push
cd grimnir_radio.wiki
git add .
git commit -m "Initial wiki documentation"
git push origin master
```

## 🔄 Keeping Wiki Updated

### Workflow

1. **Edit** files in `wiki/` directory
2. **Commit** to main repository
3. **Publish** using `./scripts/publish-wiki.sh`

### Automated Sync (Optional)

Create `.github/workflows/sync-wiki.yml` for automatic publishing:

```yaml
name: Sync Wiki

on:
  push:
    branches: [main]
    paths:
      - 'wiki/**'

jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Sync to Wiki
        run: ./scripts/publish-wiki.sh
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## 📝 Pages to Create Next

The following stub pages should be created when content is ready:

**Core Concepts:**
- `wiki/Smart-Blocks.md` - Smart block system guide
- `wiki/Clock-Scheduling.md` - Clock scheduling system
- `wiki/Priority-System.md` - 5-tier priority system
- `wiki/Live-Broadcasting.md` - DJ live broadcasting

**Deployment:**
- `wiki/Docker-Deployment.md` - Docker guide (can copy from docs/DOCKER_DEPLOYMENT.md)
- `wiki/Nix-Installation.md` - Nix guide (can copy from docs/NIX_INSTALLATION.md)
- `wiki/Production-Deployment.md` - Production checklist
- `wiki/Multi-Instance.md` - Horizontal scaling

**Integration:**
- `wiki/API-Reference.md` - REST API reference
- `wiki/WebSocket-Events.md` - WebSocket API
- `wiki/Migration-Guide.md` - Migrate from AzuraCast/LibreTime

**Operations:**
- `wiki/Observability.md` - Monitoring and metrics
- `wiki/Database-Optimization.md` - Database tuning
- `wiki/Troubleshooting.md` - Common issues
- `wiki/Performance-Tuning.md` - Performance optimization
- `wiki/Backup-Restore.md` - Backup strategies
- `wiki/Upgrading.md` - Version upgrade guide

**Development:**
- `wiki/Development.md` - Development guide
- `wiki/Engineering-Spec.md` - Technical specification

## 🎯 Quick Create Commands

To create remaining pages from existing docs:

```bash
# Copy Docker deployment
cp docs/DOCKER_DEPLOYMENT.md wiki/Docker-Deployment.md

# Copy Nix installation
cp docs/NIX_INSTALLATION.md wiki/Nix-Installation.md

# Copy API reference
cp docs/API_REFERENCE.md wiki/API-Reference.md

# Copy WebSocket events
cp docs/WEBSOCKET_EVENTS.md wiki/WebSocket-Events.md

# Copy migration guide
cp docs/MIGRATION.md wiki/Migration-Guide.md

# Copy observability
cp docs/OBSERVABILITY.md wiki/Observability.md

# Copy database optimization
cp docs/DATABASE_OPTIMIZATION.md wiki/Database-Optimization.md

# Copy multi-instance
cp docs/MULTI_INSTANCE.md wiki/Multi-Instance.md

# Copy production deployment
cp docs/PRODUCTION_DEPLOYMENT.md wiki/Production-Deployment.md

# Copy output encoding
cp docs/OUTPUT_ENCODING.md wiki/Output-Encoding.md

# Copy engineering spec
cp docs/specs/ENGINEERING_SPEC.md wiki/Engineering-Spec.md

# Copy sales spec (if wanted)
cp docs/specs/SALES_SPEC.md wiki/Sales-Spec.md

# After copying, run publish script
./scripts/publish-wiki.sh
```

## 🔧 Wiki Maintenance

### Adding New Pages

1. Create `wiki/New-Page.md`
2. Add link to `wiki/_Sidebar.md`
3. Link from relevant pages
4. Run `./scripts/publish-wiki.sh`

### Updating Pages

1. Edit file in `wiki/` directory
2. Commit to git
3. Run `./scripts/publish-wiki.sh`

### Images

```bash
# Create images directory
mkdir -p wiki/images

# Add images
cp path/to/image.png wiki/images/

# Reference in markdown
![Diagram](images/image.png)

# Publish
./scripts/publish-wiki.sh
```

## 📊 Wiki Status

| Page | Status | Notes |
|------|--------|-------|
| Home | ✅ Complete | Landing page with overview |
| Getting Started | ✅ Complete | Quick start guide |
| Installation | ✅ Complete | Docker, Nix, source |
| Architecture | ✅ Complete | System design and components |
| Configuration | ✅ Complete | Environment variables |
| CHANGELOG | ✅ Complete | Version history |
| Roadmap | ✅ Complete | v1.1.0 plans |
| _Sidebar | ✅ Complete | Navigation |
| Smart Blocks | ⏳ Pending | Need to create |
| Clock Scheduling | ⏳ Pending | Need to create |
| Priority System | ⏳ Pending | Need to create |
| Live Broadcasting | ⏳ Pending | Need to create |
| Docker Deployment | ⏳ Pending | Copy from docs |
| Nix Installation | ⏳ Pending | Copy from docs |
| Production | ⏳ Pending | Copy from docs |
| Multi-Instance | ⏳ Pending | Copy from docs |
| API Reference | ⏳ Pending | Copy from docs |
| WebSocket Events | ⏳ Pending | Copy from docs |
| Migration Guide | ⏳ Pending | Copy from docs |
| Observability | ⏳ Pending | Copy from docs |
| Database Optimization | ⏳ Pending | Copy from docs |
| Troubleshooting | ⏳ Pending | Need to create |
| Development | ⏳ Pending | Need to create |
| Engineering Spec | ⏳ Pending | Copy from docs |

## 🎓 Benefits of Wiki Structure

### For Users

- **Single source of truth** - All documentation in one place
- **GitHub integration** - Search, history, contributions
- **Easy navigation** - Sidebar and search
- **Mobile-friendly** - GitHub wiki responsive design
- **Version history** - Track documentation changes

### For Developers

- **Markdown in repository** - Version controlled, reviewable
- **Automated publishing** - Script handles sync
- **Easy updates** - Edit locally, commit, publish
- **No duplication** - Wiki is master, docs/ for dev reference
- **Clear structure** - Organized by user journey

### For Contributors

- **Clear contribution path** - Edit wiki/*.md, submit PR
- **Preview locally** - Test with grip or other MD viewers
- **Consistent format** - Templates and examples
- **Review process** - PRs ensure quality

## 🚀 Next Steps

1. **Copy remaining documentation:**
   ```bash
   # Run commands from "Quick Create Commands" section above
   ```

2. **Review and edit copied content:**
   - Remove outdated sections
   - Update links to point to wiki pages
   - Add "last updated" dates
   - Simplify for user audience

3. **Publish to GitHub:**
   ```bash
   ./scripts/publish-wiki.sh
   ```

4. **Set up GitHub Action** (optional):
   - Create `.github/workflows/sync-wiki.yml`
   - Auto-publish on push to main

5. **Update main README:**
   - Link to wiki: `https://github.com/friendsincode/grimnir_radio/wiki`
   - Mention documentation location

## 📚 Resources

- [GitHub Wiki Docs](https://docs.github.com/en/communities/documenting-your-project-with-wikis)
- [GitHub Flavored Markdown](https://github.github.com/gfm/)
- [wiki/README.md](wiki/README.md) - Detailed maintenance guide

## ✅ Completion Checklist

- [x] Create wiki directory structure
- [x] Write Home.md
- [x] Write Getting-Started.md
- [x] Write Installation.md
- [x] Write Architecture.md
- [x] Write Configuration.md
- [x] Create _Sidebar.md
- [x] Copy CHANGELOG.md
- [x] Copy Roadmap.md
- [x] Create publish script
- [x] Copy remaining documentation files ✅ (2026-01-23)
- [x] Update internal wiki links ✅ (2026-01-23)
- [x] Test publish script ✅ (2026-01-23)
- [x] Publish to GitHub wiki ✅ (2026-01-23)
- [ ] Set up GitHub Action (optional)
- [ ] Update main README
- [ ] Announce wiki to users
