#!/usr/bin/env bash
#
# Grimnir Radio - Build and Push Docker Images
#
# Usage:
#   ./scripts/docker-build-push.sh              # Build and push current version
#   ./scripts/docker-build-push.sh v1.15.14     # Build and push specific version
#   ./scripts/docker-build-push.sh --build-only # Build without pushing
#
# Requires:
#   - Docker with buildx support
#   - GITHUB_TOKEN environment variable (PAT with packages:write scope)
#     OR already logged in to ghcr.io
#

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
REGISTRY="ghcr.io"
ORG="friendsincode"
RADIO_IMAGE="${REGISTRY}/${ORG}/grimnir_radio"
MEDIAENGINE_IMAGE="${REGISTRY}/${ORG}/grimnir_mediaengine"

# Parse arguments
BUILD_ONLY=false
VERSION=""

for arg in "$@"; do
    case $arg in
        --build-only)
            BUILD_ONLY=true
            ;;
        v*)
            VERSION="$arg"
            ;;
        *)
            echo -e "${RED}Unknown argument: $arg${NC}"
            exit 1
            ;;
    esac
done

# Get version from version.go if not specified
if [ -z "$VERSION" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
    VERSION=$(grep 'var Version' "$PROJECT_ROOT/internal/version/version.go" | sed 's/.*"\(.*\)".*/\1/')
    if [ -z "$VERSION" ]; then
        echo -e "${RED}Could not determine version${NC}"
        exit 1
    fi
fi

# Strip 'v' prefix for tagging if present
VERSION_NUM="${VERSION#v}"

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}  Grimnir Radio Docker Build${NC}"
echo -e "${BLUE}======================================${NC}"
echo -e "  Version: ${GREEN}${VERSION_NUM}${NC}"
echo -e "  Build only: ${BUILD_ONLY}"
echo ""

# Change to project root
cd "$(dirname "$0")/.."

# Check if logged in to ghcr.io (only if pushing)
if [ "$BUILD_ONLY" = false ]; then
    if ! docker info 2>/dev/null | grep -q "ghcr.io"; then
        if [ -n "$GITHUB_TOKEN" ]; then
            echo -e "${YELLOW}Logging in to ghcr.io...${NC}"
            echo "$GITHUB_TOKEN" | docker login ghcr.io -u "${GITHUB_USER:-$USER}" --password-stdin
        else
            echo -e "${YELLOW}Checking ghcr.io authentication...${NC}"
            if ! docker pull ghcr.io/friendsincode/grimnir_radio:latest >/dev/null 2>&1; then
                echo -e "${RED}Not logged in to ghcr.io${NC}"
                echo -e "Either set GITHUB_TOKEN environment variable or run:"
                echo -e "  docker login ghcr.io -u YOUR_USERNAME"
                exit 1
            fi
        fi
    fi
fi

# Build Grimnir Radio (control plane)
echo ""
echo -e "${BLUE}Building Grimnir Radio...${NC}"
docker build \
    -t "${RADIO_IMAGE}:${VERSION_NUM}" \
    -t "${RADIO_IMAGE}:latest" \
    --build-arg VERSION="v${VERSION_NUM}" \
    -f Dockerfile \
    .

echo -e "${GREEN}✓ Grimnir Radio built${NC}"

# Build Media Engine
echo ""
echo -e "${BLUE}Building Media Engine...${NC}"
docker build \
    -t "${MEDIAENGINE_IMAGE}:${VERSION_NUM}" \
    -t "${MEDIAENGINE_IMAGE}:latest" \
    --build-arg VERSION="v${VERSION_NUM}" \
    -f Dockerfile.mediaengine \
    .

echo -e "${GREEN}✓ Media Engine built${NC}"

# Push if not build-only
if [ "$BUILD_ONLY" = false ]; then
    echo ""
    echo -e "${BLUE}Pushing images to ${REGISTRY}...${NC}"

    docker push "${RADIO_IMAGE}:${VERSION_NUM}"
    docker push "${RADIO_IMAGE}:latest"
    echo -e "${GREEN}✓ Grimnir Radio pushed${NC}"

    docker push "${MEDIAENGINE_IMAGE}:${VERSION_NUM}"
    docker push "${MEDIAENGINE_IMAGE}:latest"
    echo -e "${GREEN}✓ Media Engine pushed${NC}"
fi

echo ""
echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}  Build Complete!${NC}"
echo -e "${GREEN}======================================${NC}"
echo ""
echo "Images:"
echo "  ${RADIO_IMAGE}:${VERSION_NUM}"
echo "  ${MEDIAENGINE_IMAGE}:${VERSION_NUM}"

if [ "$BUILD_ONLY" = false ]; then
    echo ""
    echo "To deploy on your server:"
    echo "  ./grimnir pull && ./grimnir up -d"
fi
