#!/bin/bash
# Publish wiki documentation to GitHub Wiki

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Configuration
REPO="${GITHUB_REPOSITORY:-friendsincode/grimnir_radio}"
WIKI_DIR="wiki"
TEMP_DIR=$(mktemp -d)
USE_HTTPS=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --https)
            USE_HTTPS=true
            shift
            ;;
        --repo)
            REPO="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --https         Use HTTPS instead of SSH (default: SSH)"
            echo "  --repo REPO     Specify repository (default: friendsincode/grimnir_radio)"
            echo "  -h, --help      Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                    # Use SSH (default)"
            echo "  $0 --https            # Use HTTPS"
            echo "  $0 --repo user/repo   # Custom repository"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

echo -e "${GREEN}Grimnir Radio Wiki Publisher${NC}"
echo "Repository: $REPO"

# Determine clone URL
if [ "$USE_HTTPS" = true ]; then
    WIKI_URL="https://github.com/${REPO}.wiki.git"
    echo "Protocol: HTTPS"
else
    WIKI_URL="git@github.com:${REPO}.wiki.git"
    echo "Protocol: SSH"
fi
echo

# Check if wiki directory exists
if [ ! -d "$WIKI_DIR" ]; then
    echo -e "${RED}Error: wiki/ directory not found${NC}"
    exit 1
fi

# Count markdown files
MD_COUNT=$(find "$WIKI_DIR" -maxdepth 1 -name "*.md" | wc -l)
echo "Found $MD_COUNT markdown files to publish"
echo

# Clone wiki repository
echo -e "${YELLOW}Cloning wiki repository...${NC}"
if ! git clone "$WIKI_URL" "$TEMP_DIR" 2>&1; then
    echo
    echo -e "${RED}Error: Failed to clone wiki repository${NC}"
    echo
    echo "Possible causes:"
    echo "  1. Wiki not initialized - create the first page on GitHub"
    echo "  2. SSH key not configured (if using SSH)"
    echo "  3. No access permissions to repository"
    echo
    if [ "$USE_HTTPS" = false ]; then
        echo "Try using HTTPS instead: $0 --https"
        echo "Or configure SSH key: ssh-keygen -t ed25519 -C \"your_email@example.com\""
    fi
    echo
    rm -rf "$TEMP_DIR"
    exit 1
fi

# Copy markdown files
echo -e "${YELLOW}Copying wiki files...${NC}"
cp -v "$WIKI_DIR"/*.md "$TEMP_DIR/" | sed 's/^/  /'

# Create images directory if needed
if [ -d "$WIKI_DIR/images" ]; then
    echo -e "${YELLOW}Copying images...${NC}"
    mkdir -p "$TEMP_DIR/images"
    cp -rv "$WIKI_DIR/images"/* "$TEMP_DIR/images/" | sed 's/^/  /'
fi

# Navigate to wiki repo
cd "$TEMP_DIR"

# Configure git
git config user.name "Wiki Publisher"
git config user.email "wiki@grimnir.radio"

# Add all files
git add .

# Check for changes
if git diff --staged --quiet; then
    echo
    echo -e "${YELLOW}No changes to publish${NC}"
    echo "Wiki is already up to date"
    cd - > /dev/null
    rm -rf "$TEMP_DIR"
    exit 0
fi

# Show what changed
echo
echo -e "${YELLOW}Changes to be published:${NC}"
git diff --staged --stat

# Commit
echo
echo -e "${YELLOW}Creating commit...${NC}"
git commit -m "Update wiki from main repository

Updated $(date '+%Y-%m-%d %H:%M:%S')

Files changed: $(git diff --staged --name-only | wc -l)"

# Push
echo
echo -e "${YELLOW}Pushing to GitHub Wiki...${NC}"
if git push origin master; then
    echo
    echo -e "${GREEN}✓ Wiki updated successfully!${NC}"
    echo
    echo "View at: https://github.com/${REPO}/wiki"
else
    echo
    echo -e "${RED}✗ Failed to push to wiki${NC}"
    echo "You may need to:"
    echo "  1. Check if you have push access"
    echo "  2. Initialize the wiki by creating the first page on GitHub"
    echo "  3. Configure git credentials"
    cd - > /dev/null
    rm -rf "$TEMP_DIR"
    exit 1
fi

# Cleanup
cd - > /dev/null
rm -rf "$TEMP_DIR"

echo
echo -e "${GREEN}Done!${NC}"
