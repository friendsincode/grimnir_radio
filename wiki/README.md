# Grimnir Radio Wiki

This directory contains the Grimnir Radio documentation in wiki format, designed to be pushed to GitHub's wiki.

## Wiki Structure

```
wiki/
├── Home.md                    # Landing page
├── _Sidebar.md               # Navigation sidebar
├── Getting-Started.md        # Quick start guide
├── Installation.md           # Installation methods
├── Architecture.md           # System architecture
├── Configuration.md          # Configuration reference
├── API-Reference.md          # REST API docs
├── Smart-Blocks.md           # Smart block guide
├── Clock-Scheduling.md       # Clock system guide
├── Priority-System.md        # Priority ladder docs
├── Live-Broadcasting.md      # Live DJ guide
├── Docker-Deployment.md      # Docker guide
├── Nix-Installation.md       # Nix installation
├── Production-Deployment.md  # Production checklist
├── Multi-Instance.md         # Horizontal scaling
├── Migration-Guide.md        # Migrate from AzuraCast/LibreTime
├── Observability.md          # Monitoring & metrics
├── Database-Optimization.md  # Database tuning
├── WebSocket-Events.md       # WebSocket API
├── Output-Encoding.md        # Audio encoding
├── Troubleshooting.md        # Common issues
├── Development.md            # Development guide
├── Engineering-Spec.md       # Technical specification
├── CHANGELOG.md              # Version history
└── Roadmap.md                # Future plans
```

## Publishing to GitHub Wiki

GitHub wikis are separate git repositories. Here's how to publish this content:

### Method 1: GitHub Web UI (Easiest)

1. Go to your repository: `https://github.com/friendsincode/grimnir_radio`
2. Click the **Wiki** tab
3. Click **Create the first page** (if new) or **New Page**
4. Copy content from `wiki/Home.md` and paste
5. Set page title to "Home"
6. Click **Save Page**
7. Repeat for each `.md` file in the `wiki/` directory

### Method 2: Clone Wiki Repository (Bulk Upload)

```bash
# Clone the wiki repository
git clone https://github.com/friendsincode/grimnir_radio.wiki.git

# Copy wiki files
cp -r wiki/*.md grimnir_radio.wiki/

# Commit and push
cd grimnir_radio.wiki
git add .
git commit -m "Update documentation"
git push origin master
```

### Method 3: Automated Script

```bash
#!/bin/bash
# scripts/publish-wiki.sh

# Configuration
REPO="friendsincode/grimnir_radio"
WIKI_DIR="wiki"
TEMP_DIR=$(mktemp -d)

# Clone wiki
git clone "https://github.com/${REPO}.wiki.git" "$TEMP_DIR"

# Copy files
cp -r "$WIKI_DIR"/*.md "$TEMP_DIR/"

# Commit and push
cd "$TEMP_DIR"
git add .
if git diff --staged --quiet; then
  echo "No changes to publish"
else
  git commit -m "Update wiki from main repository"
  git push origin master
  echo "Wiki updated successfully"
fi

# Cleanup
rm -rf "$TEMP_DIR"
```

Make executable and run:
```bash
chmod +x scripts/publish-wiki.sh
./scripts/publish-wiki.sh
```

## Keeping Wiki in Sync

### Option 1: Manual Sync

Edit files in `wiki/` directory, then run the publish script.

### Option 2: GitHub Actions (Automated)

Create `.github/workflows/sync-wiki.yml`:

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
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          git clone "https://${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.wiki.git" wiki-repo
          cp -r wiki/*.md wiki-repo/
          cd wiki-repo
          git config user.name "GitHub Actions"
          git config user.email "actions@github.com"
          git add .
          if git diff --staged --quiet; then
            echo "No changes"
          else
            git commit -m "Auto-sync from main repository"
            git push
          fi
```

### Option 3: Git Submodule

```bash
# Initialize wiki as submodule
git clone https://github.com/friendsincode/grimnir_radio.wiki.git wiki-published
git submodule add https://github.com/friendsincode/grimnir_radio.wiki.git wiki-published

# Copy and push
cp wiki/*.md wiki-published/
cd wiki-published
git add .
git commit -m "Update wiki"
git push
```

## Wiki Conventions

### File Naming

- Use PascalCase for file names: `Getting-Started.md`
- Wiki links use the same format: `[Getting Started](Getting-Started)`
- Spaces in titles become hyphens in filenames

### Internal Links

```markdown
# Absolute (from wiki root)
[Installation Guide](Installation)

# With anchor
[Docker Installation](Installation#docker-compose-installation)

# External
[GitHub](https://github.com/friendsincode/grimnir_radio)
```

### Code Blocks

Use language-specific syntax highlighting:

````markdown
```bash
./grimnirradio serve
```

```go
func main() {
    // Go code
}
```

```json
{
  "key": "value"
}
```
````

### Images

Store images in `wiki/images/` (GitHub wikis support image uploads):

```markdown
![Architecture Diagram](images/architecture.png)
```

Or use external URLs:
```markdown
![Logo](https://example.com/logo.png)
```

## Maintenance

### Adding New Pages

1. Create `wiki/New-Page.md`
2. Add to `_Sidebar.md` navigation
3. Link from relevant pages
4. Run publish script

### Updating Existing Pages

1. Edit file in `wiki/` directory
2. Commit changes to main repository
3. Run publish script (or GitHub Action triggers automatically)

### Removing Pages

1. Delete file from `wiki/` directory
2. Remove from `_Sidebar.md`
3. Update any links pointing to the page
4. Run publish script
5. Manually delete from GitHub wiki if needed

## Best Practices

### Documentation Quality

- **Clear Headings**: Use hierarchical headings (H1 > H2 > H3)
- **Code Examples**: Include working code snippets
- **Step-by-Step**: Number steps for tutorials
- **Troubleshooting**: Add "Common Issues" sections
- **Links**: Link to related pages
- **Update Dates**: Note when content was last updated

### Content Organization

- **Home**: Overview and navigation
- **Getting Started**: Fastest path to success
- **Guides**: Task-oriented how-tos
- **Reference**: Complete technical details
- **Troubleshooting**: Problem-solution format

### Writing Style

- **Concise**: Short paragraphs, bullet points
- **Active Voice**: "Run the command" not "The command should be run"
- **Examples**: Show, don't just tell
- **Audience**: Assume basic technical knowledge
- **Consistency**: Use consistent terminology

## Search and Navigation

GitHub wiki features:
- **Search box**: Full-text search
- **Sidebar**: Quick navigation (defined in `_Sidebar.md`)
- **Page history**: Git history per page
- **Clone wiki**: Download as git repo

## Versioning

Wiki content tracks the main branch. For version-specific docs:

```markdown
## Installation (v1.0.0)

Instructions for the current stable release...

## Installation (v0.9.x Legacy)

<details>
<summary>Click to expand legacy instructions</summary>

Old installation steps...
</details>
```

Or create version branches in the wiki repo:
```bash
cd wiki-published
git checkout -b v1.0
# Edit pages
git push origin v1.0
```

## Contributing

To improve documentation:

1. Edit files in `wiki/` directory
2. Test locally (use `grip` or similar Markdown renderer)
3. Commit to feature branch
4. Create pull request
5. After merge, wiki auto-syncs (if GitHub Action configured)

## Local Preview

Use `grip` to preview wiki pages locally:

```bash
# Install grip
pip install grip

# Preview a page
grip wiki/Home.md

# Opens browser at http://localhost:6419
```

Or use any Markdown viewer that supports GitHub Flavored Markdown.

## Troubleshooting

### Wiki Not Appearing

- Ensure repository has wiki enabled (Settings > Features)
- Check that wiki repository exists: `https://github.com/USER/REPO.wiki`

### Images Not Loading

- Use relative paths from wiki root: `images/diagram.png`
- Or upload directly through GitHub wiki web UI
- External URLs must be publicly accessible

### Links Broken

- Use filename without `.md` extension: `[Link](Page-Name)`
- Check capitalization (case-sensitive)
- Ensure referenced page exists

### Sync Failing

- Check GitHub token permissions (workflow)
- Ensure wiki is initialized (create first page manually)
- Verify git credentials

## Resources

- [GitHub Wiki Documentation](https://docs.github.com/en/communities/documenting-your-project-with-wikis)
- [GitHub Flavored Markdown](https://github.github.com/gfm/)
- [Mermaid Diagrams](https://mermaid-js.github.io/) - Supported in GitHub wikis
