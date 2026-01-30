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
# - Configuration validation and auto-fix
#
# Usage:
#   ./docker-quick-start.sh          # Interactive deployment
#   ./docker-quick-start.sh --check  # Check config for missing/weak values
#   ./docker-quick-start.sh --fix    # Check and fix config automatically
#   ./docker-quick-start.sh --stop   # Stop services
#   ./docker-quick-start.sh --clean  # Remove all data (destructive)
#   ./docker-quick-start.sh --help   # Show help
#
# Docker Network:
#   This script assumes all services communicate over the 'grimnir-network'
#   Docker bridge network using service hostnames:
#   - postgres:5432     (PostgreSQL database)
#   - redis:6379        (Redis for events/leader election)
#   - mediaengine:9091  (Media Engine gRPC)
#   - icecast:8000      (Icecast streaming server)

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

# Required configuration keys with their Docker network defaults
# Format: KEY|DEFAULT_VALUE|DESCRIPTION|PROMPT_TYPE (text|password|bool)
declare -a REQUIRED_CONFIG=(
    # Core passwords (no defaults - must be generated or provided)
    "POSTGRES_PASSWORD||PostgreSQL password|password"
    "REDIS_PASSWORD||Redis password|password"
    "JWT_SIGNING_KEY||JWT signing key for authentication|password"
    "ICECAST_ADMIN_PASSWORD||Icecast admin password|password"
    "ICECAST_SOURCE_PASSWORD||Icecast source password|password"

    # Database - assumes grimnir-network
    "GRIMNIR_DB_BACKEND|postgres|Database backend (postgres/mysql/sqlite)|text"
    "GRIMNIR_DB_DSN|host=postgres port=5432 user=grimnir password=\${POSTGRES_PASSWORD} dbname=grimnir sslmode=disable|Database connection string|text"

    # Redis - assumes grimnir-network
    "GRIMNIR_REDIS_ADDR|redis:6379|Redis address|text"
    "GRIMNIR_REDIS_DB|0|Redis database number|text"

    # Media Engine - assumes grimnir-network
    "GRIMNIR_MEDIA_ENGINE_GRPC_ADDR|mediaengine:9091|Media Engine gRPC address|text"

    # Icecast - assumes grimnir-network
    "GRIMNIR_ICECAST_URL|http://icecast:8000|Internal Icecast URL|text"

    # Media storage
    "GRIMNIR_MEDIA_ROOT|/var/lib/grimnir/media|Media files root path|text"
    "GRIMNIR_MEDIA_BACKEND|filesystem|Media storage backend (filesystem/s3)|text"

    # Server settings
    "GRIMNIR_HTTP_PORT|8080|HTTP API port|text"
    "GRIMNIR_HTTP_BIND|0.0.0.0|HTTP bind address|text"

    # Environment
    "ENVIRONMENT|production|Environment (development/staging/production)|text"
    "LOG_LEVEL|info|Log level (debug/info/warn/error)|text"

    # Icecast settings
    "ICECAST_ADMIN_USERNAME|admin|Icecast admin username|text"
    "ICECAST_HOSTNAME|localhost|Icecast hostname|text"
    "ICECAST_LOCATION|Earth|Icecast location|text"
    "ICECAST_MAX_CLIENTS|100|Maximum Icecast clients|text"
    "ICECAST_MAX_SOURCES|10|Maximum Icecast sources|text"

    # Optional but recommended
    "LEADER_ELECTION_ENABLED|false|Enable multi-instance leader election|bool"
    "GRIMNIR_INSTANCE_ID|grimnir-1|Instance identifier|text"
    "TRACING_ENABLED|false|Enable OpenTelemetry tracing|bool"
)

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

# =============================================================================
# Configuration Check Functions
# =============================================================================

# Get value from .env file
get_env_value() {
    local key="$1"
    local env_file="$2"

    if [ -f "$env_file" ]; then
        # Extract value, handling quoted values and comments
        local value=$(grep -E "^${key}=" "$env_file" 2>/dev/null | head -1 | cut -d'=' -f2- | sed 's/^["'"'"']//;s/["'"'"']$//' | sed 's/#.*//' | xargs)
        echo "$value"
    fi
}

# Check if a value is set and non-empty
is_value_set() {
    local value="$1"
    [ -n "$value" ] && [ "$value" != '""' ] && [ "$value" != "''" ]
}

# Parse config entry
parse_config_entry() {
    local entry="$1"
    local field="$2"  # 1=key, 2=default, 3=description, 4=type

    echo "$entry" | cut -d'|' -f"$field"
}

# Check existing .env file for missing configuration
check_env_config() {
    local env_file="$1"
    local missing_keys=()
    local weak_keys=()

    if [ ! -f "$env_file" ]; then
        print_error "No .env file found at: $env_file"
        return 1
    fi

    print_section "Checking Configuration"
    print_info "Analyzing: $env_file"
    echo ""

    local total_keys=0
    local configured_keys=0
    local missing_count=0
    local weak_count=0

    for entry in "${REQUIRED_CONFIG[@]}"; do
        local key=$(parse_config_entry "$entry" 1)
        local default_val=$(parse_config_entry "$entry" 2)
        local description=$(parse_config_entry "$entry" 3)
        local prompt_type=$(parse_config_entry "$entry" 4)

        ((total_keys++))

        local current_value=$(get_env_value "$key" "$env_file")

        if ! is_value_set "$current_value"; then
            # Key is missing or empty
            missing_keys+=("$entry")
            ((missing_count++))
            print_error "Missing: $key"
            print_info "  → $description"
        else
            ((configured_keys++))

            # Check for weak/default passwords
            if [ "$prompt_type" = "password" ]; then
                if [[ "$current_value" == *"change"* ]] || \
                   [[ "$current_value" == *"hackme"* ]] || \
                   [[ "$current_value" == *"secret"* ]] || \
                   [ ${#current_value} -lt 16 ]; then
                    weak_keys+=("$entry")
                    ((weak_count++))
                    print_warning "Weak:    $key (insecure value detected)"
                else
                    print_success "OK:      $key"
                fi
            else
                print_success "OK:      $key"
            fi
        fi
    done

    echo ""
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "  Configuration Summary"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "  Total keys checked:  $total_keys"
    echo -e "  ${GREEN}Configured:${NC}          $configured_keys"
    [ $missing_count -gt 0 ] && echo -e "  ${RED}Missing:${NC}             $missing_count"
    [ $weak_count -gt 0 ] && echo -e "  ${YELLOW}Weak passwords:${NC}      $weak_count"
    echo ""

    # Store for later use
    MISSING_CONFIG_KEYS=("${missing_keys[@]}")
    WEAK_CONFIG_KEYS=("${weak_keys[@]}")

    if [ $missing_count -eq 0 ] && [ $weak_count -eq 0 ]; then
        print_success "Configuration is complete and secure!"
        return 0
    fi

    return 1
}

# Prompt for missing configuration values
prompt_missing_config() {
    local env_file="$1"

    if [ ${#MISSING_CONFIG_KEYS[@]} -eq 0 ] && [ ${#WEAK_CONFIG_KEYS[@]} -eq 0 ]; then
        return 0
    fi

    print_section "Update Configuration"

    local updates=()

    # Handle missing keys
    if [ ${#MISSING_CONFIG_KEYS[@]} -gt 0 ]; then
        print_info "The following required settings are missing:"
        echo ""

        for entry in "${MISSING_CONFIG_KEYS[@]}"; do
            local key=$(parse_config_entry "$entry" 1)
            local default_val=$(parse_config_entry "$entry" 2)
            local description=$(parse_config_entry "$entry" 3)
            local prompt_type=$(parse_config_entry "$entry" 4)

            # Expand variables in default value
            local expanded_default=$(eval echo "$default_val")

            local new_value=""

            case "$prompt_type" in
                password)
                    if [ -z "$expanded_default" ]; then
                        # Generate a password if no default
                        local generated=$(generate_password)
                        if prompt_yn "Generate secure password for $key?" "y"; then
                            new_value="$generated"
                            print_success "Generated: $key"
                        else
                            prompt "$description" "" "new_value" "true"
                        fi
                    else
                        prompt "$description" "$expanded_default" "new_value" "true"
                    fi
                    ;;
                bool)
                    if prompt_yn "$description" "${expanded_default:-n}"; then
                        new_value="true"
                    else
                        new_value="false"
                    fi
                    ;;
                *)
                    prompt "$description" "$expanded_default" "new_value"
                    ;;
            esac

            if [ -n "$new_value" ]; then
                updates+=("$key=$new_value")
            fi
        done
    fi

    # Handle weak passwords
    if [ ${#WEAK_CONFIG_KEYS[@]} -gt 0 ]; then
        echo ""
        print_warning "The following passwords appear insecure:"
        echo ""

        for entry in "${WEAK_CONFIG_KEYS[@]}"; do
            local key=$(parse_config_entry "$entry" 1)
            local description=$(parse_config_entry "$entry" 3)

            if prompt_yn "Generate new secure password for $key?" "y"; then
                local new_value=$(generate_password)
                updates+=("$key=$new_value")
                print_success "Generated new password for $key"
            fi
        done
    fi

    # Apply updates
    if [ ${#updates[@]} -gt 0 ]; then
        echo ""
        print_section "Applying Updates"

        # Backup existing file
        cp "$env_file" "${env_file}.backup.$(date +%Y%m%d_%H%M%S)"
        print_info "Backed up existing .env file"

        for update in "${updates[@]}"; do
            local key="${update%%=*}"
            local value="${update#*=}"

            # Check if key exists in file
            if grep -q "^${key}=" "$env_file" 2>/dev/null; then
                # Update existing key
                # Use different delimiters for sed to handle special chars
                sed -i "s|^${key}=.*|${key}=${value}|" "$env_file"
            else
                # Add new key
                echo "${key}=${value}" >> "$env_file"
            fi
            print_success "Updated: $key"
        done

        echo ""
        print_success "Configuration updated!"
        print_info "Backup saved with .backup.* extension"
    fi
}

# Interactive configuration check mode
run_config_check() {
    print_header
    configure_deploy_dir

    if [ ! -f "$ENV_FILE" ]; then
        print_error "No .env file found at: $ENV_FILE"
        echo ""
        if prompt_yn "Would you like to create a new configuration?" "y"; then
            configure_deployment
            save_config
            create_env_file
            create_override_file
            print_success "Configuration created!"
        fi
        return
    fi

    if check_env_config "$ENV_FILE"; then
        echo ""
        print_success "Your configuration is complete!"
        echo ""
        if prompt_yn "Would you like to view the current settings?" "n"; then
            echo ""
            print_section "Current Configuration"
            grep -v '^#' "$ENV_FILE" | grep -v '^$' | sort
        fi
    else
        echo ""
        if prompt_yn "Would you like to fix the missing/weak configuration?" "y"; then
            prompt_missing_config "$ENV_FILE"

            # Re-check after updates
            echo ""
            check_env_config "$ENV_FILE"
        fi
    fi
}

# Verify Docker network assumptions
verify_docker_network_config() {
    local env_file="$1"

    print_section "Verifying Docker Network Configuration"

    local issues=()

    # Check that internal services use Docker network hostnames
    local media_engine_addr=$(get_env_value "GRIMNIR_MEDIA_ENGINE_GRPC_ADDR" "$env_file")
    local redis_addr=$(get_env_value "GRIMNIR_REDIS_ADDR" "$env_file")
    local db_dsn=$(get_env_value "GRIMNIR_DB_DSN" "$env_file")
    local icecast_url=$(get_env_value "GRIMNIR_ICECAST_URL" "$env_file")

    # Check Media Engine address
    if is_value_set "$media_engine_addr"; then
        if [[ "$media_engine_addr" == "localhost"* ]] || [[ "$media_engine_addr" == "127.0.0.1"* ]]; then
            issues+=("GRIMNIR_MEDIA_ENGINE_GRPC_ADDR uses localhost - should be 'mediaengine:9091' for Docker network")
        elif [[ "$media_engine_addr" == "mediaengine:"* ]]; then
            print_success "Media Engine: Using Docker network hostname"
        fi
    fi

    # Check Redis address
    if is_value_set "$redis_addr"; then
        if [[ "$redis_addr" == "localhost"* ]] || [[ "$redis_addr" == "127.0.0.1"* ]]; then
            issues+=("GRIMNIR_REDIS_ADDR uses localhost - should be 'redis:6379' for Docker network")
        elif [[ "$redis_addr" == "redis:"* ]]; then
            print_success "Redis: Using Docker network hostname"
        fi
    fi

    # Check Database DSN
    if is_value_set "$db_dsn"; then
        if [[ "$db_dsn" == *"host=localhost"* ]] || [[ "$db_dsn" == *"host=127.0.0.1"* ]]; then
            issues+=("GRIMNIR_DB_DSN uses localhost - should use 'host=postgres' for Docker network")
        elif [[ "$db_dsn" == *"host=postgres"* ]]; then
            print_success "Database: Using Docker network hostname"
        fi
    fi

    # Check Icecast URL
    if is_value_set "$icecast_url"; then
        if [[ "$icecast_url" == *"localhost"* ]] || [[ "$icecast_url" == *"127.0.0.1"* ]]; then
            issues+=("GRIMNIR_ICECAST_URL uses localhost - should be 'http://icecast:8000' for Docker network")
        elif [[ "$icecast_url" == *"icecast:"* ]]; then
            print_success "Icecast: Using Docker network hostname"
        fi
    fi

    if [ ${#issues[@]} -gt 0 ]; then
        echo ""
        print_warning "Found Docker network configuration issues:"
        echo ""
        for issue in "${issues[@]}"; do
            print_warning "  • $issue"
        done
        echo ""

        if prompt_yn "Would you like to fix these to use Docker network hostnames?" "y"; then
            fix_docker_network_config "$env_file"
        fi
    else
        echo ""
        print_success "Docker network configuration looks correct!"
    fi
}

# Fix Docker network configuration
fix_docker_network_config() {
    local env_file="$1"

    # Backup
    cp "$env_file" "${env_file}.backup.$(date +%Y%m%d_%H%M%S)"

    local postgres_password=$(get_env_value "POSTGRES_PASSWORD" "$env_file")

    # Fix Media Engine address
    if grep -q "^GRIMNIR_MEDIA_ENGINE_GRPC_ADDR=" "$env_file"; then
        sed -i 's|^GRIMNIR_MEDIA_ENGINE_GRPC_ADDR=.*localhost.*|GRIMNIR_MEDIA_ENGINE_GRPC_ADDR=mediaengine:9091|' "$env_file"
        sed -i 's|^GRIMNIR_MEDIA_ENGINE_GRPC_ADDR=.*127\.0\.0\.1.*|GRIMNIR_MEDIA_ENGINE_GRPC_ADDR=mediaengine:9091|' "$env_file"
    fi

    # Fix Redis address
    if grep -q "^GRIMNIR_REDIS_ADDR=" "$env_file"; then
        sed -i 's|^GRIMNIR_REDIS_ADDR=.*localhost.*|GRIMNIR_REDIS_ADDR=redis:6379|' "$env_file"
        sed -i 's|^GRIMNIR_REDIS_ADDR=.*127\.0\.0\.1.*|GRIMNIR_REDIS_ADDR=redis:6379|' "$env_file"
    fi

    # Fix Database DSN (more complex - need to preserve password)
    if grep -q "^GRIMNIR_DB_DSN=" "$env_file"; then
        local current_dsn=$(get_env_value "GRIMNIR_DB_DSN" "$env_file")
        if [[ "$current_dsn" == *"localhost"* ]] || [[ "$current_dsn" == *"127.0.0.1"* ]]; then
            local new_dsn="host=postgres port=5432 user=grimnir password=${postgres_password} dbname=grimnir sslmode=disable"
            sed -i "s|^GRIMNIR_DB_DSN=.*|GRIMNIR_DB_DSN=${new_dsn}|" "$env_file"
        fi
    fi

    # Fix Icecast URL
    if grep -q "^GRIMNIR_ICECAST_URL=" "$env_file"; then
        sed -i 's|^GRIMNIR_ICECAST_URL=.*localhost.*|GRIMNIR_ICECAST_URL=http://icecast:8000|' "$env_file"
        sed -i 's|^GRIMNIR_ICECAST_URL=.*127\.0\.0\.1.*|GRIMNIR_ICECAST_URL=http://icecast:8000|' "$env_file"
    fi

    print_success "Fixed Docker network configuration"
    print_info "Backup saved with .backup.* extension"
}

# =============================================================================
# Directory Configuration
# =============================================================================

# Configure deployment directory
configure_deploy_dir() {
    print_section "Directory Configuration"

    local current_dir="$(pwd)"

    # Docker files directory
    print_info "Where should Docker configuration files be stored?"
    print_info "  (.env, docker-compose.yml, docker-compose.override.yml)"
    echo ""

    prompt "Docker config directory" "/srv/docker/grimnir_radio" "DEPLOY_DIR"

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

    prompt "Data directory" "/srv/data/grimnir_radio" "DATA_DIR"

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

    # Ask about external services FIRST so configure_ports knows what to ask
    if prompt_yn "Use external PostgreSQL database?" "n"; then
        USE_EXTERNAL_POSTGRES=true
    else
        USE_EXTERNAL_POSTGRES=false
    fi

    if prompt_yn "Use external Redis?" "n"; then
        USE_EXTERNAL_REDIS=true
    else
        USE_EXTERNAL_REDIS=false
    fi

    configure_ports
    configure_production_volumes

    if [ "$USE_EXTERNAL_POSTGRES" = true ]; then
        configure_external_postgres
    else
        configure_volumes_postgres
    fi

    if [ "$USE_EXTERNAL_REDIS" = true ]; then
        configure_external_redis
    else
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
GRIMNIR_JWT_SIGNING_KEY=$JWT_SIGNING_KEY
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

# Source directory for build context
SOURCE_DIR=""
IN_SOURCE_DIR=false

# Detect if we're running from the grimnir_radio source directory
detect_source_dir() {
    local check_dir="$1"

    # Check for indicators that this is the grimnir_radio source
    if [ -d "$check_dir/.git" ] && \
       [ -f "$check_dir/go.mod" ] && \
       [ -f "$check_dir/Dockerfile" ] && \
       [ -f "$check_dir/Dockerfile.mediaengine" ] && \
       [ -f "$check_dir/docker-compose.yml" ]; then
        # Verify it's actually grimnir_radio by checking go.mod
        if grep -q "grimnir_radio" "$check_dir/go.mod" 2>/dev/null; then
            return 0
        fi
    fi
    return 1
}

# Find and set up source directory
setup_source_dir() {
    # First check if we're IN the source directory (current working directory)
    if detect_source_dir "$(pwd)"; then
        SOURCE_DIR="$(pwd)"
        IN_SOURCE_DIR=true
        print_success "Detected: Running from source directory"
        print_info "Source: $SOURCE_DIR"
        return 0
    fi

    # Check PROJECT_ROOT (where the script lives)
    if detect_source_dir "$PROJECT_ROOT"; then
        SOURCE_DIR="$PROJECT_ROOT"
        print_success "Detected: Source directory found"
        print_info "Source: $SOURCE_DIR"
        return 0
    fi

    # No source found
    SOURCE_DIR=""
    print_info "No source directory detected - will pull images from registry"
    return 1
}

# Copy docker-compose.yml and related files to deploy directory
copy_compose_file() {
    local dst_compose="$DEPLOY_DIR/docker-compose.yml"

    # If docker-compose.yml already exists in deploy dir, we're good
    if [ -f "$dst_compose" ]; then
        print_info "Using existing docker-compose.yml in $DEPLOY_DIR"
        return 0
    fi

    # Try to copy from SOURCE_DIR
    if [ -n "$SOURCE_DIR" ] && [ -f "$SOURCE_DIR/docker-compose.yml" ]; then
        cp "$SOURCE_DIR/docker-compose.yml" "$dst_compose"
        print_success "Copied docker-compose.yml to $DEPLOY_DIR"
        return 0
    fi

    # No source - we'll need to pull, but still need compose file
    # Download from GitHub
    print_info "Downloading docker-compose.yml from GitHub..."
    if curl -fsSL "https://raw.githubusercontent.com/friendsincode/grimnir_radio/main/docker-compose.yml" -o "$dst_compose" 2>/dev/null; then
        print_success "Downloaded docker-compose.yml"
        return 0
    fi

    print_error "Could not find or download docker-compose.yml"
    print_info "Please run this script from the grimnir_radio source directory"
    print_info "Or ensure you have internet access to download from GitHub"
    exit 1
}

# Track whether using registry images
USE_REGISTRY_IMAGES=false

# Pull or build images
build_images() {
    print_section "Docker Images"

    copy_compose_file

    # If we're in the source directory, build by default
    if [ "$IN_SOURCE_DIR" = true ]; then
        echo ""
        print_info "Source code detected - defaulting to build from source"
        echo ""
        echo "Image source options:"
        echo "  1) Build from source (recommended - you have the code)"
        echo "  2) Pull from GitHub Container Registry"
        echo ""
        prompt "Select image source [1-2]" "1" "image_source"

        if [ "$image_source" = "1" ]; then
            build_from_source
        else
            pull_from_registry
        fi
    else
        # No source, pull from registry
        echo ""
        if [ -n "$SOURCE_DIR" ]; then
            print_info "Source available at: $SOURCE_DIR"
            echo ""
            echo "Image source options:"
            echo "  1) Pull from GitHub Container Registry (recommended)"
            echo "  2) Build from source"
            echo ""
            prompt "Select image source [1-2]" "1" "image_source"

            if [ "$image_source" = "2" ]; then
                build_from_source
            else
                pull_from_registry
            fi
        else
            print_info "No source code found - pulling from registry"
            pull_from_registry
        fi
    fi
}

# Build images from source
build_from_source() {
    USE_REGISTRY_IMAGES=false

    if [ -z "$SOURCE_DIR" ] || [ ! -f "$SOURCE_DIR/Dockerfile" ]; then
        print_error "Cannot build: source directory with Dockerfiles not found"
        exit 1
    fi

    print_info "Building from source: $SOURCE_DIR"
    cd "$SOURCE_DIR"

    if [ -z "$COMPOSE_CMD" ]; then
        detect_compose_cmd
    fi

    print_info "Building images (this may take several minutes)..."
    print_info "Using BuildKit for faster builds with caching"
    DOCKER_BUILDKIT=1 COMPOSE_DOCKER_CLI_BUILD=1 $COMPOSE_CMD build --parallel

    print_success "Images built successfully"

    # Go back to deploy directory
    cd "$DEPLOY_DIR"
}

# Pull images from GitHub Container Registry
pull_from_registry() {
    USE_REGISTRY_IMAGES=true
    cd "$DEPLOY_DIR"

    # Create production compose file with ghcr.io images
    create_prod_compose_file

    print_info "Pulling images from ghcr.io..."
    get_compose_cmd pull
    print_success "Images pulled successfully"
}

# Determine which docker compose command to use
COMPOSE_CMD=""
detect_compose_cmd() {
    if docker compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
    elif command -v docker-compose >/dev/null 2>&1; then
        COMPOSE_CMD="docker-compose"
    else
        print_error "Docker Compose not found"
        exit 1
    fi
}

# Get the docker-compose command with appropriate files
get_compose_cmd() {
    local cmd="$1"
    if [ -z "$COMPOSE_CMD" ]; then
        detect_compose_cmd
    fi

    if [ "$USE_REGISTRY_IMAGES" = true ] && [ -f "$DEPLOY_DIR/docker-compose.prod.yml" ]; then
        $COMPOSE_CMD -f docker-compose.yml -f docker-compose.prod.yml -f docker-compose.override.yml $cmd
    else
        $COMPOSE_CMD $cmd
    fi
}

# Create production compose file with ghcr.io images
create_prod_compose_file() {
    cat > "$DEPLOY_DIR/docker-compose.prod.yml" <<'EOF'
# Production override - use pre-built images from GitHub Container Registry
# Generated by docker-quick-start.sh
#
# This file overrides the build: directives with image: to pull from ghcr.io

services:
  mediaengine:
    image: ghcr.io/friendsincode/grimnir_mediaengine:latest

  grimnir:
    image: ghcr.io/friendsincode/grimnir_radio:latest
EOF

    print_success "Created docker-compose.prod.yml"

    # Create helper script for easier management
    cat > "$DEPLOY_DIR/grimnir" <<SCRIPT
#!/bin/bash
# Grimnir Radio - Docker Compose Wrapper
# Usage: ./grimnir [command]
#   ./grimnir up -d      Start services
#   ./grimnir down       Stop services
#   ./grimnir logs -f    Follow logs
#   ./grimnir ps         Show status
#   ./grimnir pull       Pull latest images
#   ./grimnir reset-db   Reset database to fresh state (DESTRUCTIVE)

cd "\$(dirname "\$0")"

# Data directory from deployment
DATA_DIR="$DATA_DIR"

# Use docker compose (v2) if available, else docker-compose
if docker compose version >/dev/null 2>&1; then
    COMPOSE="docker compose"
else
    COMPOSE="docker-compose"
fi

COMPOSE_FILES="-f docker-compose.yml -f docker-compose.prod.yml -f docker-compose.override.yml"

case "\$1" in
    reset-db)
        echo ""
        echo "============================================================"
        echo "  WARNING: THIS ACTION IS NOT RECOVERABLE!"
        echo "============================================================"
        echo ""
        echo "  This will PERMANENTLY DELETE all database data including:"
        echo "    - All users and accounts"
        echo "    - All stations and configurations"
        echo "    - All media library metadata"
        echo "    - All playlists, schedules, and history"
        echo ""
        echo "  Data directory: \$DATA_DIR/postgres-data"
        echo ""
        echo "============================================================"
        echo ""
        read -p "Type 'yes' to confirm deletion: " confirm
        if [ "\$confirm" = "yes" ]; then
            echo "Stopping services..."
            \$COMPOSE \$COMPOSE_FILES down
            echo "Removing postgres data..."
            sudo rm -rf "\$DATA_DIR/postgres-data"
            echo "Starting services..."
            \$COMPOSE \$COMPOSE_FILES up -d
            echo ""
            echo "Database reset complete. Visit the web UI to run setup wizard."
        else
            echo "Aborted."
        fi
        ;;
    *)
        \$COMPOSE \$COMPOSE_FILES "\$@"
        ;;
esac
SCRIPT

    chmod +x "$DEPLOY_DIR/grimnir"
    print_success "Created grimnir helper script"
}

# Start services
start_services() {
    print_section "Starting Services"

    cd "$DEPLOY_DIR"

    print_info "Starting Grimnir Radio stack..."
    get_compose_cmd "up -d"

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
        if get_compose_cmd "ps" | grep -q "unhealthy\|starting"; then
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
    print_info "Check status with: cd $DEPLOY_DIR && docker-compose ps"
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
    if [ "$USE_REGISTRY_IMAGES" = true ]; then
        echo -e "  Registry:      $DEPLOY_DIR/docker-compose.prod.yml"
        echo -e "  Helper:        $DEPLOY_DIR/grimnir"
    fi
    echo -e "  Saved Config:  $CONFIG_FILE"
    echo ""

    echo -e "${CYAN}Deployment Directory:${NC}"
    echo -e "  $DEPLOY_DIR"
    echo ""

    echo -e "${CYAN}Useful Commands:${NC} (run from $DEPLOY_DIR)"
    if [ "$USE_REGISTRY_IMAGES" = true ]; then
        echo -e "  View logs:       ./grimnir logs -f"
        echo -e "  Stop services:   ./grimnir down"
        echo -e "  Restart:         ./grimnir restart"
        echo -e "  Status:          ./grimnir ps"
        echo -e "  Pull updates:    ./grimnir pull && ./grimnir up -d"
    else
        echo -e "  View logs:       docker-compose logs -f"
        echo -e "  Stop services:   docker-compose down"
        echo -e "  Restart:         docker-compose restart"
        echo -e "  Status:          docker-compose ps"
    fi
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

    # Check if using registry images
    if [ -f "$DEPLOY_DIR/docker-compose.prod.yml" ]; then
        USE_REGISTRY_IMAGES=true
    fi

    get_compose_cmd "down"

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

    # Check if using registry images
    if [ -f "$DEPLOY_DIR/docker-compose.prod.yml" ]; then
        USE_REGISTRY_IMAGES=true
    fi

    print_info "Stopping and removing containers..."
    get_compose_cmd "down -v"

    if [ -f "$ENV_FILE" ]; then
        rm "$ENV_FILE"
        print_info "Removed $ENV_FILE"
    fi

    if [ -f "$OVERRIDE_FILE" ]; then
        rm "$OVERRIDE_FILE"
        print_info "Removed $OVERRIDE_FILE"
    fi

    if [ -f "$DEPLOY_DIR/docker-compose.prod.yml" ]; then
        rm "$DEPLOY_DIR/docker-compose.prod.yml"
        print_info "Removed docker-compose.prod.yml"
    fi

    if [ -d "$CONFIG_DIR" ]; then
        rm -rf "$CONFIG_DIR"
        print_info "Removed $CONFIG_DIR"
    fi

    print_success "Cleanup complete"
}

# Show usage help
show_help() {
    echo "Grimnir Radio - Docker Deployment Script"
    echo ""
    echo "Usage: $0 [OPTION]"
    echo ""
    echo "Options:"
    echo "  (none)      Interactive deployment (default)"
    echo "  --check     Check existing configuration for missing/weak values"
    echo "  --fix       Check and automatically fix configuration issues"
    echo "  --stop      Stop running services"
    echo "  --clean     Stop services and remove all data (DESTRUCTIVE)"
    echo "  --help      Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                    # Start interactive deployment"
    echo "  $0 --check            # Check configuration for issues"
    echo "  $0 --fix              # Check and fix configuration"
    echo "  $0 --stop             # Stop all services"
    echo ""
    echo "The script assumes services communicate over the 'grimnir-network'"
    echo "Docker network using service hostnames (postgres, redis, mediaengine, icecast)."
    echo ""
}

# Main
main() {
    case "${1:-}" in
        --help|-h)
            show_help
            exit 0
            ;;
        --stop)
            stop_services
            ;;
        --clean)
            clean_all
            ;;
        --check)
            run_config_check
            ;;
        --fix)
            print_header
            configure_deploy_dir

            if [ ! -f "$ENV_FILE" ]; then
                print_error "No .env file found at: $ENV_FILE"
                echo ""
                if prompt_yn "Would you like to create a new configuration?" "y"; then
                    configure_deployment
                    save_config
                    create_env_file
                    create_override_file
                    print_success "Configuration created!"
                fi
                exit 0
            fi

            # Check and fix configuration
            if ! check_env_config "$ENV_FILE"; then
                prompt_missing_config "$ENV_FILE"
            fi

            # Verify Docker network configuration
            verify_docker_network_config "$ENV_FILE"

            # Check if override file exists
            if [ ! -f "$OVERRIDE_FILE" ]; then
                echo ""
                print_warning "No docker-compose.override.yml found"
                if prompt_yn "Create docker-compose.override.yml with default settings?" "y"; then
                    # Load config values from env file for override creation
                    HTTP_PORT=$(get_env_value "GRIMNIR_HTTP_PORT" "$ENV_FILE")
                    HTTP_PORT=${HTTP_PORT:-8080}
                    METRICS_PORT=${METRICS_PORT:-9000}
                    GRPC_PORT=${GRPC_PORT:-9091}
                    ICECAST_PORT=$(get_env_value "ICECAST_PORT" "$ENV_FILE")
                    ICECAST_PORT=${ICECAST_PORT:-8000}
                    POSTGRES_PORT=${POSTGRES_PORT:-5432}
                    REDIS_PORT=${REDIS_PORT:-6379}

                    # Use DATA_DIR for paths
                    MEDIA_STORAGE_PATH="${DATA_DIR:-$DEPLOY_DIR}/media-data"
                    POSTGRES_DATA_PATH="${DATA_DIR:-$DEPLOY_DIR}/postgres-data"
                    REDIS_DATA_PATH="${DATA_DIR:-$DEPLOY_DIR}/redis-data"
                    ICECAST_LOGS_PATH="${DATA_DIR:-$DEPLOY_DIR}/icecast-logs"

                    USE_EXTERNAL_POSTGRES=false
                    USE_EXTERNAL_REDIS=false
                    ENABLE_MULTI_INSTANCE=false

                    create_override_file
                fi
            fi

            echo ""
            print_success "Configuration check complete!"
            ;;
        *)
            print_header
            check_prerequisites
            setup_source_dir
            configure_deploy_dir

            # Check for existing .env and offer to verify/update it
            if [ -f "$ENV_FILE" ]; then
                print_info "Found existing configuration at: $ENV_FILE"
                echo ""
                echo "Options:"
                echo "  1) Check and update existing configuration"
                echo "  2) Start fresh with new configuration"
                echo "  3) Use existing configuration as-is"
                echo ""
                prompt "Select option [1-3]" "1" "config_choice"

                case "$config_choice" in
                    1)
                        # Check and update
                        if ! check_env_config "$ENV_FILE"; then
                            if prompt_yn "Fix missing/weak configuration?" "y"; then
                                prompt_missing_config "$ENV_FILE"
                            fi
                        fi
                        verify_docker_network_config "$ENV_FILE"

                        # Load config for deployment
                        if [ -f "$CONFIG_FILE" ]; then
                            source "$CONFIG_FILE"
                        fi
                        ;;
                    2)
                        # Fresh configuration
                        configure_deployment
                        save_config
                        create_env_file
                        create_override_file
                        ;;
                    3)
                        # Use as-is, just load saved config
                        if [ -f "$CONFIG_FILE" ]; then
                            source "$CONFIG_FILE"
                        fi
                        ;;
                    *)
                        print_error "Invalid selection"
                        exit 1
                        ;;
                esac
            else
                # No existing config - run full setup
                if ! load_config; then
                    configure_deployment
                    save_config
                fi
                create_env_file
                create_override_file
            fi

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
