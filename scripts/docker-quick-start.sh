#!/usr/bin/env bash
#
# Grimnir Radio - Docker Production Deployment Script
#
# Interactive configuration for production deployments with support for:
# - Custom ports
# - External volume mounts (NAS/SAN storage)
# - External databases
# - Multi-instance deployment
# - Configuration persistence

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# DEPLOY_DIR and DATA_DIR will be set interactively
DEPLOY_DIR=""
DATA_DIR=""
CONFIG_DIR=""
CONFIG_FILE=""
ENV_FILE=""
OVERRIDE_FILE=""

# Default values
DEFAULT_HTTP_PORT=8080
DEFAULT_METRICS_PORT=9000
DEFAULT_GRPC_PORT=9091
DEFAULT_ICECAST_PORT=8000
DEFAULT_POSTGRES_PORT=5432
DEFAULT_REDIS_PORT=6379

# Functions
print_header() {
    clear
    echo -e "${CYAN}╔════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║                                                        ║${NC}"
    echo -e "${CYAN}║         ${GREEN}Grimnir Radio - Docker Deployment${CYAN}          ║${NC}"
    echo -e "${CYAN}║                                                        ║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

print_section() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_info() {
    echo -e "${CYAN}ℹ${NC} $1"
}

# Prompt with default value
prompt() {
    local prompt_text="$1"
    local default_value="$2"
    local var_name="$3"
    local is_password="${4:-false}"

    if [ "$is_password" = "true" ]; then
        echo -n -e "${CYAN}${prompt_text}${NC} (default: ${YELLOW}***${NC}): "
        read -s user_input
        echo ""
    else
        echo -n -e "${CYAN}${prompt_text}${NC} (default: ${YELLOW}${default_value}${NC}): "
        read user_input
    fi

    if [ -z "$user_input" ]; then
        eval "$var_name=\"$default_value\""
    else
        eval "$var_name=\"$user_input\""
    fi
}

# Prompt yes/no
prompt_yn() {
    local prompt_text="$1"
    local default_value="${2:-n}"

    while true; do
        if [ "$default_value" = "y" ]; then
            echo -n -e "${CYAN}${prompt_text}${NC} [${GREEN}Y${NC}/n]: "
        else
            echo -n -e "${CYAN}${prompt_text}${NC} [y/${RED}N${NC}]: "
        fi

        read -r answer
        answer="${answer:-$default_value}"

        case "$answer" in
            [Yy]* ) return 0;;
            [Nn]* ) return 1;;
            * ) echo "Please answer yes or no.";;
        esac
    done
}

# Generate random password
generate_password() {
    openssl rand -base64 32 | tr -d "=+/" | cut -c1-32
}

# Check if path exists and is writable
check_path() {
    local path="$1"
    local description="$2"

    if [ ! -e "$path" ]; then
        print_warning "$description does not exist: $path"
        if prompt_yn "Create directory $path?" "y"; then
            mkdir -p "$path"
            print_success "Created $path"
        else
            return 1
        fi
    fi

    if [ ! -w "$path" ]; then
        print_error "$description is not writable: $path"
        print_info "Try: sudo chown -R \$(whoami): $path"
        return 1
    fi

    return 0
}

# Validate port
validate_port() {
    local port="$1"
    if ! [[ "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
        return 1
    fi
    return 0
}

# Check if port is available
check_port() {
    local port="$1"
    local description="$2"

    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1 || \
       netstat -tuln 2>/dev/null | grep -q ":$port "; then
        print_warning "Port $port ($description) is already in use"
        return 1
    fi
    return 0
}

# Find next available port starting from given port
find_available_port() {
    local start_port="$1"
    local max_attempts="${2:-100}"

    for i in $(seq 0 $max_attempts); do
        local test_port=$((start_port + i))

        if [ $test_port -gt 65535 ]; then
            return 1
        fi

        if ! lsof -Pi :$test_port -sTCP:LISTEN -t >/dev/null 2>&1 && \
           ! netstat -tuln 2>/dev/null | grep -q ":$test_port "; then
            echo "$test_port"
            return 0
        fi
    done

    return 1
}

# Get suggested port (checks if default is available, suggests alternative if not)
suggest_port() {
    local default_port="$1"
    local description="$2"

    if lsof -Pi :$default_port -sTCP:LISTEN -t >/dev/null 2>&1 || \
       netstat -tuln 2>/dev/null | grep -q ":$default_port "; then
        # Port is in use, find alternative
        local suggested_port=$(find_available_port $default_port)

        if [ -n "$suggested_port" ]; then
            # Send messages to stderr so they don't get captured in $()
            print_warning "Port $default_port ($description) is in use" >&2
            print_info "Suggesting available port: $suggested_port" >&2
            echo "$suggested_port"
        else
            print_error "Could not find available port near $default_port" >&2
            echo "$default_port"
        fi
    else
        # Port is available
        echo "$default_port"
    fi
}

# Configure deployment directory
configure_deploy_dir() {
    print_section "Directory Configuration"

    local current_dir="$(pwd)"

    # Docker files directory
    print_info "Where should Docker configuration files be stored?"
    print_info "  (.env, docker-compose.yml, docker-compose.override.yml)"
    echo ""

    prompt "Docker config directory" "$current_dir" "DEPLOY_DIR"

    # Expand ~ to home directory
    DEPLOY_DIR="${DEPLOY_DIR/#\~/$HOME}"

    # Convert to absolute path
    if [[ "$DEPLOY_DIR" != /* ]]; then
        DEPLOY_DIR="$current_dir/$DEPLOY_DIR"
    fi

    # Check if directory exists or create it
    if [ ! -d "$DEPLOY_DIR" ]; then
        print_warning "Directory does not exist: $DEPLOY_DIR"
        if prompt_yn "Create directory?" "y"; then
            mkdir -p "$DEPLOY_DIR"
            print_success "Created $DEPLOY_DIR"
        else
            print_error "Cannot proceed without a valid directory"
            exit 1
        fi
    fi

    # Check if writable
    if [ ! -w "$DEPLOY_DIR" ]; then
        print_error "Directory is not writable: $DEPLOY_DIR"
        exit 1
    fi

    # Set docker config path variables
    CONFIG_DIR="$DEPLOY_DIR/.docker-deploy"
    CONFIG_FILE="$CONFIG_DIR/deployment.conf"
    ENV_FILE="$DEPLOY_DIR/.env"
    OVERRIDE_FILE="$DEPLOY_DIR/docker-compose.override.yml"

    print_success "Docker config: $DEPLOY_DIR"
    echo ""

    # Data directory
    print_info "Where should application data be stored?"
    print_info "  (media-data/, postgres-data/, redis-data/, icecast-logs/)"
    print_info "  This can be a separate mount point (e.g., NFS)"
    echo ""

    prompt "Data directory" "/srv/data" "DATA_DIR"

    # Expand ~ to home directory
    DATA_DIR="${DATA_DIR/#\~/$HOME}"

    # Convert to absolute path
    if [[ "$DATA_DIR" != /* ]]; then
        DATA_DIR="$current_dir/$DATA_DIR"
    fi

    # Check if directory exists or create it
    if [ ! -d "$DATA_DIR" ]; then
        print_warning "Directory does not exist: $DATA_DIR"
        if prompt_yn "Create directory?" "y"; then
            mkdir -p "$DATA_DIR"
            print_success "Created $DATA_DIR"
        else
            print_error "Cannot proceed without a valid directory"
            exit 1
        fi
    fi

    # Check if writable
    if [ ! -w "$DATA_DIR" ]; then
        print_error "Directory is not writable: $DATA_DIR"
        exit 1
    fi

    print_success "Data directory: $DATA_DIR"
    echo ""
}

# Check prerequisites
check_prerequisites() {
    print_section "Checking Prerequisites"

    local all_good=true

    if ! command -v docker >/dev/null 2>&1; then
        print_error "Docker is not installed"
        print_info "Install from: https://docs.docker.com/get-docker/"
        all_good=false
    else
        print_success "Docker is installed ($(docker --version))"
    fi

    if ! command -v docker-compose >/dev/null 2>&1 && ! docker compose version >/dev/null 2>&1; then
        print_error "Docker Compose is not installed"
        print_info "Install from: https://docs.docker.com/compose/install/"
        all_good=false
    else
        if command -v docker-compose >/dev/null 2>&1; then
            print_success "Docker Compose is installed ($(docker-compose --version))"
        else
            print_success "Docker Compose is installed ($(docker compose version))"
        fi
    fi

    if ! docker info >/dev/null 2>&1; then
        print_error "Docker daemon is not running"
        all_good=false
    else
        print_success "Docker daemon is running"
    fi

    if [ "$all_good" = false ]; then
        echo ""
        print_error "Prerequisites not met. Please fix the issues above and try again."
        exit 1
    fi

    echo ""
}

# Show port usage summary
show_port_usage() {
    print_section "Port Usage Check"

    print_info "Checking default ports for conflicts..."
    echo ""

    local ports_to_check=("$DEFAULT_HTTP_PORT:HTTP API" \
                          "$DEFAULT_METRICS_PORT:Metrics" \
                          "$DEFAULT_GRPC_PORT:gRPC" \
                          "$DEFAULT_ICECAST_PORT:Icecast" \
                          "$DEFAULT_POSTGRES_PORT:PostgreSQL" \
                          "$DEFAULT_REDIS_PORT:Redis")

    local conflicts=0

    for port_info in "${ports_to_check[@]}"; do
        local port="${port_info%%:*}"
        local service="${port_info#*:}"

        if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1 || \
           netstat -tuln 2>/dev/null | grep -q ":$port "; then
            print_warning "$service (port $port) - IN USE, will suggest alternative"
            conflicts=$((conflicts + 1))
        else
            print_success "$service (port $port) - Available"
        fi
    done

    echo ""
    if [ $conflicts -gt 0 ]; then
        print_info "Found $conflicts port conflict(s). Alternative ports will be suggested."
    else
        print_success "All default ports are available!"
    fi

    echo ""
}

# Load saved configuration
load_config() {
    if [ -f "$CONFIG_FILE" ]; then
        print_info "Found saved configuration"
        if prompt_yn "Load previous configuration?" "y"; then
            source "$CONFIG_FILE"
            print_success "Loaded configuration from $CONFIG_FILE"
            return 0
        fi
    fi
    return 1
}

# Save configuration
save_config() {
    mkdir -p "$CONFIG_DIR"

    cat > "$CONFIG_FILE" <<EOF
# Grimnir Radio Docker Deployment Configuration
# Generated: $(date)

# Deployment mode
DEPLOYMENT_MODE="$DEPLOYMENT_MODE"

# Directories
DATA_DIR="$DATA_DIR"

# Ports
HTTP_PORT=$HTTP_PORT
METRICS_PORT=$METRICS_PORT
GRPC_PORT=$GRPC_PORT
ICECAST_PORT=$ICECAST_PORT
POSTGRES_PORT=$POSTGRES_PORT
REDIS_PORT=$REDIS_PORT

# Storage paths
MEDIA_STORAGE_PATH="$MEDIA_STORAGE_PATH"
POSTGRES_DATA_PATH="$POSTGRES_DATA_PATH"
REDIS_DATA_PATH="$REDIS_DATA_PATH"
ICECAST_LOGS_PATH="$ICECAST_LOGS_PATH"

# Database configuration
USE_EXTERNAL_POSTGRES=$USE_EXTERNAL_POSTGRES
EXTERNAL_POSTGRES_HOST="$EXTERNAL_POSTGRES_HOST"
EXTERNAL_POSTGRES_PORT=$EXTERNAL_POSTGRES_PORT
EXTERNAL_POSTGRES_USER="$EXTERNAL_POSTGRES_USER"
EXTERNAL_POSTGRES_DB="$EXTERNAL_POSTGRES_DB"

# Redis configuration
USE_EXTERNAL_REDIS=$USE_EXTERNAL_REDIS
EXTERNAL_REDIS_HOST="$EXTERNAL_REDIS_HOST"
EXTERNAL_REDIS_PORT=$EXTERNAL_REDIS_PORT

# Multi-instance
ENABLE_MULTI_INSTANCE=$ENABLE_MULTI_INSTANCE
INSTANCE_COUNT=$INSTANCE_COUNT

# Passwords (saved for reference, regenerate if needed)
POSTGRES_PASSWORD="$POSTGRES_PASSWORD"
REDIS_PASSWORD="$REDIS_PASSWORD"
JWT_SIGNING_KEY="$JWT_SIGNING_KEY"
ICECAST_ADMIN_PASSWORD="$ICECAST_ADMIN_PASSWORD"
ICECAST_SOURCE_PASSWORD="$ICECAST_SOURCE_PASSWORD"
EOF

    chmod 600 "$CONFIG_FILE"
    print_success "Configuration saved to $CONFIG_FILE"
}

# Interactive configuration
configure_deployment() {
    print_section "Deployment Mode"

    echo "Select deployment type:"
    echo "  1) Quick Start (default ports, local volumes)"
    echo "  2) Custom Configuration (configure all settings)"
    echo "  3) Production (external storage, custom ports)"
    echo ""

    prompt "Select mode [1-3]" "1" "mode_choice"

    case "$mode_choice" in
        1)
            DEPLOYMENT_MODE="quick"
            configure_quick_mode
            ;;
        2)
            DEPLOYMENT_MODE="custom"
            configure_custom_mode
            ;;
        3)
            DEPLOYMENT_MODE="production"
            configure_production_mode
            ;;
        *)
            print_error "Invalid selection"
            exit 1
            ;;
    esac
}

# Quick mode configuration
configure_quick_mode() {
    print_section "Quick Start Mode"

    print_info "Using default settings with automatic port detection..."
    echo ""

    # Auto-detect available ports
    HTTP_PORT=$(suggest_port $DEFAULT_HTTP_PORT "HTTP API")
    METRICS_PORT=$(suggest_port $DEFAULT_METRICS_PORT "Prometheus metrics")
    GRPC_PORT=$(suggest_port $DEFAULT_GRPC_PORT "Media Engine gRPC")
    ICECAST_PORT=$(suggest_port $DEFAULT_ICECAST_PORT "Icecast streaming")
    POSTGRES_PORT=$(suggest_port $DEFAULT_POSTGRES_PORT "PostgreSQL")
    REDIS_PORT=$(suggest_port $DEFAULT_REDIS_PORT "Redis")

    echo ""
    print_success "Port allocation complete"
    print_info "  HTTP API:      $HTTP_PORT"
    print_info "  Metrics:       $METRICS_PORT"
    print_info "  Media Engine:  $GRPC_PORT"
    print_info "  Icecast:       $ICECAST_PORT"
    print_info "  PostgreSQL:    $POSTGRES_PORT"
    print_info "  Redis:         $REDIS_PORT"

    MEDIA_STORAGE_PATH="$DATA_DIR/media-data"
    POSTGRES_DATA_PATH="$DATA_DIR/postgres-data"
    REDIS_DATA_PATH="$DATA_DIR/redis-data"
    ICECAST_LOGS_PATH="$DATA_DIR/icecast-logs"

    USE_EXTERNAL_POSTGRES=false
    USE_EXTERNAL_REDIS=false
    ENABLE_MULTI_INSTANCE=false

    generate_passwords
}

# Custom mode configuration
configure_custom_mode() {
    print_section "Custom Configuration"

    configure_ports
    configure_volumes

    if prompt_yn "Use external PostgreSQL database?" "n"; then
        USE_EXTERNAL_POSTGRES=true
        configure_external_postgres
    else
        USE_EXTERNAL_POSTGRES=false
    fi

    if prompt_yn "Use external Redis?" "n"; then
        USE_EXTERNAL_REDIS=true
        configure_external_redis
    else
        USE_EXTERNAL_REDIS=false
    fi

    if prompt_yn "Enable multi-instance deployment?" "n"; then
        ENABLE_MULTI_INSTANCE=true
        configure_multi_instance
    else
        ENABLE_MULTI_INSTANCE=false
    fi

    generate_passwords
}

# Production mode configuration
configure_production_mode() {
    print_section "Production Configuration"

    print_info "This mode is designed for production deployments with:"
    print_info "  - External storage mounts (NAS/SAN)"
    print_info "  - Custom port mappings"
    print_info "  - Optional external databases"
    print_info "  - Multi-instance support"
    echo ""

    configure_ports
    configure_production_volumes

    if prompt_yn "Use external PostgreSQL database?" "y"; then
        USE_EXTERNAL_POSTGRES=true
        configure_external_postgres
    else
        USE_EXTERNAL_POSTGRES=false
        configure_volumes_postgres
    fi

    if prompt_yn "Use external Redis?" "n"; then
        USE_EXTERNAL_REDIS=true
        configure_external_redis
    else
        USE_EXTERNAL_REDIS=false
        configure_volumes_redis
    fi

    if prompt_yn "Enable multi-instance deployment for HA?" "y"; then
        ENABLE_MULTI_INSTANCE=true
        configure_multi_instance
    else
        ENABLE_MULTI_INSTANCE=false
    fi

    generate_passwords
}

# Configure ports
configure_ports() {
    print_section "Port Configuration"

    print_info "Checking port availability and suggesting alternatives..."
    echo ""

    # HTTP API port
    local suggested_http=$(suggest_port $DEFAULT_HTTP_PORT "HTTP API")
    while true; do
        prompt "HTTP API port" "$suggested_http" "HTTP_PORT"
        if validate_port "$HTTP_PORT"; then
            if check_port "$HTTP_PORT" "HTTP API"; then
                break
            else
                # User entered a port that's in use, suggest next available
                suggested_http=$(find_available_port $HTTP_PORT)
                if [ -n "$suggested_http" ]; then
                    print_info "Try port: $suggested_http"
                fi
            fi
        else
            print_error "Invalid port number (must be 1-65535)"
        fi
    done

    # Prometheus metrics port
    local suggested_metrics=$(suggest_port $DEFAULT_METRICS_PORT "Prometheus metrics")
    while true; do
        prompt "Prometheus metrics port" "$suggested_metrics" "METRICS_PORT"
        if validate_port "$METRICS_PORT"; then
            if check_port "$METRICS_PORT" "Metrics"; then
                break
            else
                suggested_metrics=$(find_available_port $METRICS_PORT)
                if [ -n "$suggested_metrics" ]; then
                    print_info "Try port: $suggested_metrics"
                fi
            fi
        else
            print_error "Invalid port number (must be 1-65535)"
        fi
    done

    # Media Engine gRPC port
    local suggested_grpc=$(suggest_port $DEFAULT_GRPC_PORT "Media Engine gRPC")
    while true; do
        prompt "Media Engine gRPC port" "$suggested_grpc" "GRPC_PORT"
        if validate_port "$GRPC_PORT"; then
            if check_port "$GRPC_PORT" "gRPC"; then
                break
            else
                suggested_grpc=$(find_available_port $GRPC_PORT)
                if [ -n "$suggested_grpc" ]; then
                    print_info "Try port: $suggested_grpc"
                fi
            fi
        else
            print_error "Invalid port number (must be 1-65535)"
        fi
    done

    # Icecast streaming port
    local suggested_icecast=$(suggest_port $DEFAULT_ICECAST_PORT "Icecast streaming")
    while true; do
        prompt "Icecast streaming port" "$suggested_icecast" "ICECAST_PORT"
        if validate_port "$ICECAST_PORT"; then
            if check_port "$ICECAST_PORT" "Icecast"; then
                break
            else
                suggested_icecast=$(find_available_port $ICECAST_PORT)
                if [ -n "$suggested_icecast" ]; then
                    print_info "Try port: $suggested_icecast"
                fi
            fi
        else
            print_error "Invalid port number (must be 1-65535)"
        fi
    done

    # PostgreSQL port (if using local database)
    if ! $USE_EXTERNAL_POSTGRES; then
        local suggested_postgres=$(suggest_port $DEFAULT_POSTGRES_PORT "PostgreSQL")
        while true; do
            prompt "PostgreSQL port" "$suggested_postgres" "POSTGRES_PORT"
            if validate_port "$POSTGRES_PORT"; then
                if check_port "$POSTGRES_PORT" "PostgreSQL"; then
                    break
                else
                    suggested_postgres=$(find_available_port $POSTGRES_PORT)
                    if [ -n "$suggested_postgres" ]; then
                        print_info "Try port: $suggested_postgres"
                    fi
                fi
            else
                print_error "Invalid port number (must be 1-65535)"
            fi
        done
    fi

    # Redis port (if using local Redis)
    if ! $USE_EXTERNAL_REDIS; then
        local suggested_redis=$(suggest_port $DEFAULT_REDIS_PORT "Redis")
        while true; do
            prompt "Redis port" "$suggested_redis" "REDIS_PORT"
            if validate_port "$REDIS_PORT"; then
                if check_port "$REDIS_PORT" "Redis"; then
                    break
                else
                    suggested_redis=$(find_available_port $REDIS_PORT)
                    if [ -n "$suggested_redis" ]; then
                        print_info "Try port: $suggested_redis"
                    fi
                fi
            else
                print_error "Invalid port number (must be 1-65535)"
            fi
        done
    fi

    echo ""
    print_success "Port configuration complete"
}

# Configure volumes (basic)
configure_volumes() {
    print_section "Volume Configuration"

    prompt "Media storage path" "$DATA_DIR/media-data" "MEDIA_STORAGE_PATH"
    prompt "PostgreSQL data path" "$DATA_DIR/postgres-data" "POSTGRES_DATA_PATH"
    prompt "Redis data path" "$DATA_DIR/redis-data" "REDIS_DATA_PATH"
    prompt "Icecast logs path" "$DATA_DIR/icecast-logs" "ICECAST_LOGS_PATH"
}

# Configure production volumes
configure_production_volumes() {
    print_section "Production Storage Configuration"

    print_info "Using data directory: $DATA_DIR"
    echo ""

    while true; do
        prompt "Media storage path (for audio files)" "$DATA_DIR/media-data" "MEDIA_STORAGE_PATH"
        if check_path "$MEDIA_STORAGE_PATH" "Media storage path"; then
            break
        fi
    done

    while true; do
        prompt "Icecast logs path" "$DATA_DIR/icecast-logs" "ICECAST_LOGS_PATH"
        if check_path "$ICECAST_LOGS_PATH" "Icecast logs path"; then
            break
        fi
    done
}

# Configure PostgreSQL volume
configure_volumes_postgres() {
    while true; do
        prompt "PostgreSQL data path" "$DATA_DIR/postgres-data" "POSTGRES_DATA_PATH"
        if check_path "$POSTGRES_DATA_PATH" "PostgreSQL data path"; then
            break
        fi
    done
}

# Configure Redis volume
configure_volumes_redis() {
    while true; do
        prompt "Redis data path" "$DATA_DIR/redis-data" "REDIS_DATA_PATH"
        if check_path "$REDIS_DATA_PATH" "Redis data path"; then
            break
        fi
    done
}

# Configure external PostgreSQL
configure_external_postgres() {
    print_section "External PostgreSQL Configuration"

    prompt "PostgreSQL host" "postgres.example.com" "EXTERNAL_POSTGRES_HOST"
    prompt "PostgreSQL port" "5432" "EXTERNAL_POSTGRES_PORT"
    prompt "PostgreSQL username" "grimnir" "EXTERNAL_POSTGRES_USER"
    prompt "PostgreSQL database" "grimnir" "EXTERNAL_POSTGRES_DB"
    prompt "PostgreSQL password" "" "EXTERNAL_POSTGRES_PASSWORD" "true"

    if [ -z "$EXTERNAL_POSTGRES_PASSWORD" ]; then
        EXTERNAL_POSTGRES_PASSWORD=$(generate_password)
        print_info "Generated password for PostgreSQL"
    fi
}

# Configure external Redis
configure_external_redis() {
    print_section "External Redis Configuration"

    prompt "Redis host" "redis.example.com" "EXTERNAL_REDIS_HOST"
    prompt "Redis port" "6379" "EXTERNAL_REDIS_PORT"
    prompt "Redis password" "" "EXTERNAL_REDIS_PASSWORD" "true"

    if [ -z "$EXTERNAL_REDIS_PASSWORD" ]; then
        EXTERNAL_REDIS_PASSWORD=$(generate_password)
        print_info "Generated password for Redis"
    fi
}

# Configure multi-instance
configure_multi_instance() {
    print_section "Multi-Instance Configuration"

    prompt "Number of API instances (2-10)" "3" "INSTANCE_COUNT"

    if ! [[ "$INSTANCE_COUNT" =~ ^[0-9]+$ ]] || [ "$INSTANCE_COUNT" -lt 2 ] || [ "$INSTANCE_COUNT" -gt 10 ]; then
        print_warning "Invalid instance count, using 3"
        INSTANCE_COUNT=3
    fi

    print_info "Will deploy $INSTANCE_COUNT API instances with leader election"
}

# Generate passwords
generate_passwords() {
    print_section "Security Configuration"

    if [ -z "$POSTGRES_PASSWORD" ]; then
        POSTGRES_PASSWORD=$(generate_password)
        print_success "Generated PostgreSQL password"
    fi

    if [ -z "$REDIS_PASSWORD" ]; then
        REDIS_PASSWORD=$(generate_password)
        print_success "Generated Redis password"
    fi

    if [ -z "$JWT_SIGNING_KEY" ]; then
        JWT_SIGNING_KEY=$(generate_password)
        print_success "Generated JWT signing key"
    fi

    if [ -z "$ICECAST_ADMIN_PASSWORD" ]; then
        ICECAST_ADMIN_PASSWORD=$(generate_password)
        print_success "Generated Icecast admin password"
    fi

    if [ -z "$ICECAST_SOURCE_PASSWORD" ]; then
        ICECAST_SOURCE_PASSWORD=$(generate_password)
        print_success "Generated Icecast source password"
    fi
}

# Create .env file
create_env_file() {
    print_section "Creating Environment Configuration"

    cat > "$ENV_FILE" <<EOF
# Grimnir Radio - Docker Deployment
# Generated: $(date)
# Mode: $DEPLOYMENT_MODE

# Environment
ENVIRONMENT=production
LOG_LEVEL=info

# Ports
GRIMNIR_HTTP_PORT=$HTTP_PORT
GRIMNIR_METRICS_PORT=$METRICS_PORT
ICECAST_PORT=$ICECAST_PORT

# Database
EOF

    if [ "$USE_EXTERNAL_POSTGRES" = true ]; then
        cat >> "$ENV_FILE" <<EOF
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN=host=$EXTERNAL_POSTGRES_HOST port=$EXTERNAL_POSTGRES_PORT user=$EXTERNAL_POSTGRES_USER password=$EXTERNAL_POSTGRES_PASSWORD dbname=$EXTERNAL_POSTGRES_DB sslmode=require
EOF
    else
        cat >> "$ENV_FILE" <<EOF
POSTGRES_PASSWORD=$POSTGRES_PASSWORD
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN=host=postgres port=5432 user=grimnir password=$POSTGRES_PASSWORD dbname=grimnir sslmode=disable
EOF
    fi

    cat >> "$ENV_FILE" <<EOF

# Redis
EOF

    if [ "$USE_EXTERNAL_REDIS" = true ]; then
        cat >> "$ENV_FILE" <<EOF
GRIMNIR_REDIS_ADDR=$EXTERNAL_REDIS_HOST:$EXTERNAL_REDIS_PORT
REDIS_PASSWORD=$EXTERNAL_REDIS_PASSWORD
EOF
    else
        cat >> "$ENV_FILE" <<EOF
REDIS_PASSWORD=$REDIS_PASSWORD
GRIMNIR_REDIS_ADDR=redis:6379
EOF
    fi

    cat >> "$ENV_FILE" <<EOF
GRIMNIR_REDIS_DB=0

# Media Engine
GRIMNIR_MEDIA_ENGINE_GRPC_ADDR=mediaengine:9091
MEDIAENGINE_LOG_LEVEL=info

# Authentication
JWT_SIGNING_KEY=$JWT_SIGNING_KEY
GRIMNIR_JWT_TTL_MINUTES=15

# Media Storage
GRIMNIR_MEDIA_BACKEND=filesystem
GRIMNIR_MEDIA_ROOT=/var/lib/grimnir/media

# Icecast
ICECAST_ADMIN_USERNAME=admin
ICECAST_ADMIN_PASSWORD=$ICECAST_ADMIN_PASSWORD
ICECAST_SOURCE_PASSWORD=$ICECAST_SOURCE_PASSWORD
ICECAST_RELAY_PASSWORD=$ICECAST_SOURCE_PASSWORD
ICECAST_HOSTNAME=localhost
ICECAST_LOCATION=Earth
ICECAST_ADMIN_EMAIL=admin@localhost
ICECAST_MAX_CLIENTS=100
ICECAST_MAX_SOURCES=10

# Scheduler
GRIMNIR_SCHEDULER_LOOKAHEAD=48h
GRIMNIR_SCHEDULER_TICK_INTERVAL=30s

# Observability
TRACING_ENABLED=false
OTLP_ENDPOINT=
TRACING_SAMPLE_RATE=0.1

# Multi-Instance
LEADER_ELECTION_ENABLED=$ENABLE_MULTI_INSTANCE
GRIMNIR_INSTANCE_ID=grimnir-1
EOF

    print_success "Created $ENV_FILE"
}

# Create docker-compose.override.yml
create_override_file() {
    print_section "Creating Docker Compose Override"

    cat > "$OVERRIDE_FILE" <<EOF
# Grimnir Radio - Docker Compose Override
# Generated: $(date)
# Mode: $DEPLOYMENT_MODE

version: '3.8'

services:
  grimnir:
    ports:
      - "$HTTP_PORT:8080"
      - "$METRICS_PORT:9000"
    volumes:
      - $MEDIA_STORAGE_PATH:/var/lib/grimnir/media
EOF

    if [ "$USE_EXTERNAL_POSTGRES" = true ]; then
        cat >> "$OVERRIDE_FILE" <<EOF
    depends_on:
      redis:
        condition: service_healthy
      mediaengine:
        condition: service_healthy

  postgres:
    profiles:
      - disabled
EOF
    else
        cat >> "$OVERRIDE_FILE" <<EOF

  postgres:
    ports:
      - "$POSTGRES_PORT:5432"
    volumes:
      - $POSTGRES_DATA_PATH:/var/lib/postgresql/data
EOF
    fi

    if [ "$USE_EXTERNAL_REDIS" = true ]; then
        cat >> "$OVERRIDE_FILE" <<EOF

  redis:
    profiles:
      - disabled
EOF
    else
        cat >> "$OVERRIDE_FILE" <<EOF

  redis:
    ports:
      - "$REDIS_PORT:6379"
    volumes:
      - $REDIS_DATA_PATH:/data
EOF
    fi

    cat >> "$OVERRIDE_FILE" <<EOF

  mediaengine:
    ports:
      - "$GRPC_PORT:9091"

  icecast:
    ports:
      - "$ICECAST_PORT:8000"
    volumes:
      - $ICECAST_LOGS_PATH:/var/log/icecast2
EOF

    if [ "$ENABLE_MULTI_INSTANCE" = true ]; then
        for i in $(seq 2 $INSTANCE_COUNT); do
            local http_port=$((HTTP_PORT + i - 1))
            local metrics_port=$((METRICS_PORT + i - 1))

            cat >> "$OVERRIDE_FILE" <<EOF

  grimnir-$i:
    image: grimnir_radio:latest
    container_name: grimnir_radio-$i
    environment:
      GRIMNIR_HTTP_PORT: 8080
      GRIMNIR_HTTP_BIND: 0.0.0.0
      GRIMNIR_DB_BACKEND: \${GRIMNIR_DB_BACKEND}
      GRIMNIR_DB_DSN: \${GRIMNIR_DB_DSN}
      GRIMNIR_REDIS_ADDR: \${GRIMNIR_REDIS_ADDR}
      REDIS_PASSWORD: \${REDIS_PASSWORD}
      GRIMNIR_REDIS_DB: \${GRIMNIR_REDIS_DB}
      GRIMNIR_MEDIA_ENGINE_GRPC_ADDR: mediaengine:9091
      JWT_SIGNING_KEY: \${JWT_SIGNING_KEY}
      GRIMNIR_JWT_TTL_MINUTES: \${GRIMNIR_JWT_TTL_MINUTES}
      GRIMNIR_MEDIA_BACKEND: \${GRIMNIR_MEDIA_BACKEND}
      GRIMNIR_MEDIA_ROOT: /var/lib/grimnir/media
      GRIMNIR_SCHEDULER_LOOKAHEAD: \${GRIMNIR_SCHEDULER_LOOKAHEAD}
      GRIMNIR_SCHEDULER_TICK_INTERVAL: \${GRIMNIR_SCHEDULER_TICK_INTERVAL}
      LEADER_ELECTION_ENABLED: "true"
      GRIMNIR_INSTANCE_ID: grimnir-$i
      GRIMNIR_ENVIRONMENT: \${ENVIRONMENT}
      LOG_LEVEL: \${LOG_LEVEL}
    ports:
      - "$http_port:8080"
      - "$metrics_port:9000"
    volumes:
      - $MEDIA_STORAGE_PATH:/var/lib/grimnir/media
    networks:
      - grimnir-network
    depends_on:
EOF

            if [ "$USE_EXTERNAL_POSTGRES" = false ]; then
                cat >> "$OVERRIDE_FILE" <<EOF
      postgres:
        condition: service_healthy
EOF
            fi

            if [ "$USE_EXTERNAL_REDIS" = false ]; then
                cat >> "$OVERRIDE_FILE" <<EOF
      redis:
        condition: service_healthy
EOF
            fi

            cat >> "$OVERRIDE_FILE" <<EOF
      mediaengine:
        condition: service_healthy
    restart: unless-stopped
EOF
        done
    fi

    # Remove volumes section if using external services
    if [ "$USE_EXTERNAL_POSTGRES" = false ] || [ "$USE_EXTERNAL_REDIS" = false ]; then
        cat >> "$OVERRIDE_FILE" <<EOF

volumes:
EOF
        if [ "$USE_EXTERNAL_POSTGRES" = false ]; then
            cat >> "$OVERRIDE_FILE" <<EOF
  postgres-data:
    driver: local
EOF
        fi
        if [ "$USE_EXTERNAL_REDIS" = false ]; then
            cat >> "$OVERRIDE_FILE" <<EOF
  redis-data:
    driver: local
EOF
        fi
    fi

    print_success "Created $OVERRIDE_FILE"
}

# Copy docker-compose.yml to deploy directory if needed
copy_compose_file() {
    local dst_compose="$DEPLOY_DIR/docker-compose.yml"

    # If docker-compose.yml already exists in deploy dir, we're done
    if [ -f "$dst_compose" ]; then
        return 0
    fi

    # Try to find it in PROJECT_ROOT (where the script came from)
    local src_compose="$PROJECT_ROOT/docker-compose.yml"

    if [ -f "$src_compose" ]; then
        cp "$src_compose" "$dst_compose"
        print_success "Copied docker-compose.yml to $DEPLOY_DIR"
        return 0
    fi

    # Not found - ask user for the grimnir_radio source location
    print_warning "docker-compose.yml not found"
    print_info "Please provide the path to the Grimnir Radio source directory"
    echo ""

    local source_dir=""
    while true; do
        prompt "Grimnir Radio source directory" "" "source_dir"

        # Expand ~ to home directory
        source_dir="${source_dir/#\~/$HOME}"

        if [ -f "$source_dir/docker-compose.yml" ]; then
            cp "$source_dir/docker-compose.yml" "$dst_compose"
            print_success "Copied docker-compose.yml to $DEPLOY_DIR"
            return 0
        else
            print_error "docker-compose.yml not found in $source_dir"
            print_info "Make sure you point to the grimnir_radio project root"
        fi
    done
}

# Build images
build_images() {
    print_section "Building Docker Images"

    copy_compose_file
    cd "$DEPLOY_DIR"

    print_info "Building images (this may take several minutes)..."
    docker-compose build --parallel

    print_success "Images built successfully"
}

# Start services
start_services() {
    print_section "Starting Services"

    cd "$DEPLOY_DIR"

    print_info "Starting Grimnir Radio stack..."
    docker-compose up -d

    print_success "Services started"
}

# Wait for health
wait_for_health() {
    print_section "Waiting for Services"

    cd "$DEPLOY_DIR"

    local max_wait=120
    local elapsed=0

    print_info "Waiting for all services to be healthy (max ${max_wait}s)..."

    while [ $elapsed -lt $max_wait ]; do
        if docker-compose ps | grep -q "unhealthy\|starting"; then
            echo -n "."
            sleep 2
            elapsed=$((elapsed + 2))
        else
            echo ""
            print_success "All services are healthy"
            return 0
        fi
    done

    echo ""
    print_warning "Some services may not be fully healthy yet"
    print_info "Check status with: docker-compose ps"
}

# Display summary
display_summary() {
    print_section "Deployment Complete!"

    echo -e "${GREEN}╔════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                                                        ║${NC}"
    echo -e "${GREEN}║         Grimnir Radio is now running!                  ║${NC}"
    echo -e "${GREEN}║                                                        ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════╝${NC}"
    echo ""

    # Show port change warnings if applicable
    local ports_changed=false
    if [ "$HTTP_PORT" != "$DEFAULT_HTTP_PORT" ] || \
       [ "$METRICS_PORT" != "$DEFAULT_METRICS_PORT" ] || \
       [ "$GRPC_PORT" != "$DEFAULT_GRPC_PORT" ] || \
       [ "$ICECAST_PORT" != "$DEFAULT_ICECAST_PORT" ]; then
        ports_changed=true
        echo -e "${YELLOW}NOTE: Some ports were changed from defaults due to conflicts:${NC}"
        [ "$HTTP_PORT" != "$DEFAULT_HTTP_PORT" ] && echo -e "  HTTP API: $DEFAULT_HTTP_PORT → $HTTP_PORT"
        [ "$METRICS_PORT" != "$DEFAULT_METRICS_PORT" ] && echo -e "  Metrics: $DEFAULT_METRICS_PORT → $METRICS_PORT"
        [ "$GRPC_PORT" != "$DEFAULT_GRPC_PORT" ] && echo -e "  gRPC: $DEFAULT_GRPC_PORT → $GRPC_PORT"
        [ "$ICECAST_PORT" != "$DEFAULT_ICECAST_PORT" ] && echo -e "  Icecast: $DEFAULT_ICECAST_PORT → $ICECAST_PORT"
        if [ "$USE_EXTERNAL_POSTGRES" = false ]; then
            [ "$POSTGRES_PORT" != "$DEFAULT_POSTGRES_PORT" ] && echo -e "  PostgreSQL: $DEFAULT_POSTGRES_PORT → $POSTGRES_PORT"
        fi
        if [ "$USE_EXTERNAL_REDIS" = false ]; then
            [ "$REDIS_PORT" != "$DEFAULT_REDIS_PORT" ] && echo -e "  Redis: $DEFAULT_REDIS_PORT → $REDIS_PORT"
        fi
        echo ""
    fi

    echo -e "${CYAN}Service URLs:${NC}"
    echo -e "  API:           http://localhost:$HTTP_PORT"
    echo -e "  Metrics:       http://localhost:$METRICS_PORT/metrics"
    echo -e "  Icecast:       http://localhost:$ICECAST_PORT"
    echo -e "  Icecast Admin: http://localhost:$ICECAST_PORT/admin"
    echo ""

    if [ "$ENABLE_MULTI_INSTANCE" = true ]; then
        echo -e "${CYAN}Additional API Instances:${NC}"
        for i in $(seq 2 $INSTANCE_COUNT); do
            local http_port=$((HTTP_PORT + i - 1))
            echo -e "  Instance $i:    http://localhost:$http_port"
        done
        echo ""
    fi

    echo -e "${CYAN}Credentials:${NC}"
    echo -e "  Icecast Admin: admin / ${ICECAST_ADMIN_PASSWORD}"
    echo ""

    echo -e "${CYAN}Storage Locations:${NC}"
    echo -e "  Media:         $MEDIA_STORAGE_PATH"
    if [ "$USE_EXTERNAL_POSTGRES" = false ]; then
        echo -e "  PostgreSQL:    $POSTGRES_DATA_PATH"
    fi
    if [ "$USE_EXTERNAL_REDIS" = false ]; then
        echo -e "  Redis:         $REDIS_DATA_PATH"
    fi
    echo -e "  Icecast Logs:  $ICECAST_LOGS_PATH"
    echo ""

    echo -e "${CYAN}Configuration Files:${NC}"
    echo -e "  Environment:   $ENV_FILE"
    echo -e "  Override:      $OVERRIDE_FILE"
    echo -e "  Saved Config:  $CONFIG_FILE"
    echo ""

    echo -e "${CYAN}Deployment Directory:${NC}"
    echo -e "  $DEPLOY_DIR"
    echo ""

    echo -e "${CYAN}Useful Commands:${NC} (run from $DEPLOY_DIR)"
    echo -e "  View logs:       docker-compose logs -f"
    echo -e "  Stop services:   docker-compose down"
    echo -e "  Restart:         docker-compose restart"
    echo -e "  Status:          docker-compose ps"
    echo ""

    echo -e "${CYAN}Next Steps:${NC}"
    echo -e "  1. Create admin user (see docs/API_REFERENCE.md)"
    echo -e "  2. Create station and mounts"
    echo -e "  3. Upload media files to: $MEDIA_STORAGE_PATH"
    echo -e "  4. Configure smart blocks and schedule"
    echo ""

    echo -e "${YELLOW}For detailed documentation, see:${NC}"
    echo -e "  docs/DOCKER_DEPLOYMENT.md"
    echo -e "  docs/API_REFERENCE.md"
    echo ""
}

# Stop services
stop_services() {
    print_header
    configure_deploy_dir
    print_section "Stopping Services"

    cd "$DEPLOY_DIR"
    docker-compose down

    print_success "Services stopped"
}

# Clean everything
clean_all() {
    print_header
    configure_deploy_dir
    print_section "Clean Deployment"

    print_warning "This will:"
    print_warning "  - Stop all services"
    print_warning "  - Remove all containers and volumes"
    print_warning "  - Delete configuration files"
    print_warning "  - PERMANENTLY DELETE ALL DATA"
    echo ""

    if ! prompt_yn "Are you absolutely sure?" "n"; then
        print_info "Aborted"
        exit 0
    fi

    echo ""
    if ! prompt_yn "Type 'yes' to confirm data deletion" "n"; then
        print_info "Aborted"
        exit 0
    fi

    cd "$DEPLOY_DIR"

    print_info "Stopping and removing containers..."
    docker-compose down -v

    if [ -f "$ENV_FILE" ]; then
        rm "$ENV_FILE"
        print_info "Removed $ENV_FILE"
    fi

    if [ -f "$OVERRIDE_FILE" ]; then
        rm "$OVERRIDE_FILE"
        print_info "Removed $OVERRIDE_FILE"
    fi

    if [ -d "$CONFIG_DIR" ]; then
        rm -rf "$CONFIG_DIR"
        print_info "Removed $CONFIG_DIR"
    fi

    print_success "Cleanup complete"
}

# Main
main() {
    case "${1:-}" in
        --stop)
            stop_services
            ;;
        --clean)
            clean_all
            ;;
        *)
            print_header
            check_prerequisites
            configure_deploy_dir

            # Try to load saved config
            if ! load_config; then
                show_port_usage
                configure_deployment
                save_config
            fi

            create_env_file
            create_override_file

            echo ""
            print_info "Configuration complete. Review the settings above."
            echo ""

            if ! prompt_yn "Proceed with deployment?" "y"; then
                print_info "Aborted. Run again to reconfigure."
                exit 0
            fi

            build_images
            start_services
            wait_for_health
            display_summary
            ;;
    esac
}

# Run
main "$@"
