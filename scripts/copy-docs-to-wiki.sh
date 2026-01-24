#!/bin/bash
# Copy documentation from docs/ to wiki/ directory

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}Copying documentation to wiki...${NC}"
echo

# Create wiki directory if it doesn't exist
mkdir -p wiki

# Copy deployment guides
echo -e "${YELLOW}Copying deployment guides...${NC}"
cp docs/DOCKER_DEPLOYMENT.md wiki/Docker-Deployment.md && echo "  ✓ Docker-Deployment.md"
cp docs/NIX_INSTALLATION.md wiki/Nix-Installation.md && echo "  ✓ Nix-Installation.md"
cp docs/PRODUCTION_DEPLOYMENT.md wiki/Production-Deployment.md && echo "  ✓ Production-Deployment.md"
cp docs/MULTI_INSTANCE.md wiki/Multi-Instance.md && echo "  ✓ Multi-Instance.md"

# Copy API and integration docs
echo -e "${YELLOW}Copying API and integration docs...${NC}"
cp docs/API_REFERENCE.md wiki/API-Reference.md && echo "  ✓ API-Reference.md"
cp docs/WEBSOCKET_EVENTS.md wiki/WebSocket-Events.md && echo "  ✓ WebSocket-Events.md"
cp docs/MIGRATION.md wiki/Migration-Guide.md && echo "  ✓ Migration-Guide.md"
cp docs/OUTPUT_ENCODING.md wiki/Output-Encoding.md && echo "  ✓ Output-Encoding.md"

# Copy operations docs
echo -e "${YELLOW}Copying operations docs...${NC}"
cp docs/OBSERVABILITY.md wiki/Observability.md && echo "  ✓ Observability.md"
cp docs/DATABASE_OPTIMIZATION.md wiki/Database-Optimization.md && echo "  ✓ Database-Optimization.md"
cp docs/ALERTING.md wiki/Alerting.md && echo "  ✓ Alerting.md"

# Copy technical specs
echo -e "${YELLOW}Copying technical specifications...${NC}"
cp docs/specs/ENGINEERING_SPEC.md wiki/Engineering-Spec.md && echo "  ✓ Engineering-Spec.md"
cp docs/specs/PROGRAMMERS_SPEC.md wiki/Programmers-Spec.md && echo "  ✓ Programmers-Spec.md"
cp docs/specs/SALES_SPEC.md wiki/Sales-Spec.md && echo "  ✓ Sales-Spec.md"

# Copy implementation details
echo -e "${YELLOW}Copying implementation details...${NC}"
cp docs/CROSSFADE_IMPLEMENTATION.md wiki/Crossfade-Implementation.md && echo "  ✓ Crossfade-Implementation.md"
cp docs/GSTREAMER_PROCESS_MANAGEMENT.md wiki/GStreamer-Process-Management.md && echo "  ✓ GStreamer-Process-Management.md"
cp docs/TELEMETRY_STREAMING.md wiki/Telemetry-Streaming.md && echo "  ✓ Telemetry-Streaming.md"

# Copy Docker quick start guide (if not already)
if [ ! -f wiki/Docker-Quick-Start.md ]; then
    cp docs/DOCKER_QUICK_START_GUIDE.md wiki/Docker-Quick-Start.md && echo "  ✓ Docker-Quick-Start.md"
fi

echo
echo -e "${GREEN}Documentation copied successfully!${NC}"
echo
echo "Files copied to wiki/ directory:"
ls -1 wiki/*.md | wc -l | xargs echo "  Total pages:"
echo
echo "Next steps:"
echo "  1. Review and edit copied files for wiki format"
echo "  2. Update internal links to point to wiki pages"
echo "  3. Run: ./scripts/publish-wiki.sh"
echo
