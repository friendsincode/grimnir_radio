{ mkShell
, go
, gopls
, gotools
, go-tools
, protobuf
, protoc-gen-go
, protoc-gen-go-grpc
, pkg-config
, gst_all_1
, postgresql
, redis
, docker-compose
, kubectl
, k9s
, gnumake
, git
, jq
, yq-go
, curl
, k6
}:

mkShell {
  name = "grimnir-radio-dev";

  buildInputs = [
    # Go development
    go
    gopls
    gotools
    go-tools

    # Protocol Buffers
    protobuf
    protoc-gen-go
    protoc-gen-go-grpc

    # GStreamer (for media engine development)
    pkg-config
    gst_all_1.gstreamer
    gst_all_1.gst-plugins-base
    gst_all_1.gst-plugins-good
    gst_all_1.gst-plugins-bad
    gst_all_1.gst-plugins-ugly
    gst_all_1.gst-libav
    gst_all_1.gst-devtools

    # Infrastructure (optional - can run separately)
    postgresql
    redis

    # Container & Orchestration tools
    docker-compose
    kubectl
    k9s

    # Build tools
    gnumake
    git

    # Utilities
    jq
    yq-go
    curl

    # Load testing
    k6
  ];

  shellHook = ''
    echo "🎙️  Grimnir Radio Development Environment"
    echo ""
    echo "Available commands:"
    echo "  make build          - Build both binaries"
    echo "  make test           - Run tests"
    echo "  make proto          - Generate protobuf code"
    echo "  make run-control    - Run control plane"
    echo "  make run-media      - Run media engine"
    echo "  make dev-db         - Start PostgreSQL (Docker)"
    echo "  make dev-redis      - Start Redis (Docker)"
    echo "  make dev-stack      - Start full stack (Docker Compose)"
    echo ""
    echo "Go version: $(go version | cut -d' ' -f3)"
    echo "Protobuf version: $(protoc --version)"
    echo "GStreamer version: $(gst-launch-1.0 --version | head -n1)"
    echo ""
    echo "Environment variables:"
    echo "  DATABASE_URL - Set to your PostgreSQL connection string"
    echo "  REDIS_URL - Set to your Redis connection string"
    echo "  MEDIA_ENGINE_GRPC_ADDR - Media engine address (default: localhost:9091)"
    echo ""

    # Set up Go environment
    export GOPATH="$HOME/go"
    export PATH="$GOPATH/bin:$PATH"

    # Set up GStreamer plugin paths
    export GST_PLUGIN_PATH_1_0="$GST_PLUGIN_PATH_1_0"
    export GST_PLUGIN_SYSTEM_PATH_1_0="$GST_PLUGIN_SYSTEM_PATH_1_0"

    # Default development database URL
    export DATABASE_URL=''${DATABASE_URL:-"postgres://grimnir:grimnir@localhost:5432/grimnir?sslmode=disable"}
    export REDIS_URL=''${REDIS_URL:-"redis://localhost:6379/0"}

    # Create .env if it doesn't exist
    if [ ! -f .env ]; then
      echo "Creating .env file from template..."
      if [ -f .env.example ]; then
        cp .env.example .env
        echo "✓ Created .env file - please review and update"
      fi
    fi
  '';

  # Prevent Nix from garbage collecting build dependencies
  preferLocalBuild = true;
}
