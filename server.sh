#!/usr/bin/env bash

source "$(dirname "$0")/utils.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

ACTION="$(echo "${1:-}" | tr '[:upper:]' '[:lower:]')"

require_command jq

chmod +x "$SCRIPT_DIR"/*.sh

### Parameters ###

# Parse optional flags
FORCE_SSL=0
export FORCE_SSL

POSITIONAL=()
shift # remove action
for arg in "$@"; do
    case "$arg" in
        --force-ssl) FORCE_SSL=1 ;;
        *) POSITIONAL+=("$arg") ;;
    esac
done

DOMAINS="${POSITIONAL[0]:-}"
read -ra domains_list <<< "$DOMAINS"
CUSTOM_PATH="${POSITIONAL[1]:-/home}"

require_file "$SCRIPT_DIR/.env.example"

if [[ ! -f "$SCRIPT_DIR/.env" ]]; then
    echo "No .env file found."
    echo "Creating .env file..."
    echo
    cp "$SCRIPT_DIR/.env.example" "$SCRIPT_DIR/.env"
    chmod 660 "$SCRIPT_DIR/.env"
    echo "Please set the user and group in the .env file."
    exit 1
fi

load_env "$SCRIPT_DIR/.env"

require_env_var USER
require_env_var GROUP

# Generate MariaDB root password if not set
if [[ -z "${MARIADB_ROOT_PASSWORD:-}" ]]; then
    MARIADB_ROOT_PASSWORD="$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)"
    if grep -q "^MARIADB_ROOT_PASSWORD=" "$SCRIPT_DIR/.env"; then
        sed_inplace "s|^MARIADB_ROOT_PASSWORD=.*|MARIADB_ROOT_PASSWORD=$MARIADB_ROOT_PASSWORD|" "$SCRIPT_DIR/.env"
    else
        echo "MARIADB_ROOT_PASSWORD=$MARIADB_ROOT_PASSWORD" >> "$SCRIPT_DIR/.env"
    fi
    log_info "MariaDB password generated and saved to .env"
fi
export MARIADB_ROOT_PASSWORD

### Port bindings ###

if [[ "${EXPOSE_SERVICES:-false}" == "true" ]]; then
    export DB_PORT="3306:3306"
    export PMA_PORT="8080:80"
    export REDIS_PORT="6379:6379"
else
    export DB_PORT="127.0.0.1:3306:3306"
    export PMA_PORT="127.0.0.1:8080:80"
    export REDIS_PORT="127.0.0.1:6379:6379"
fi

### Check if required files exist ###

require_file "$SCRIPT_DIR/check_files.sh"

"$SCRIPT_DIR/check_files.sh"
"$SCRIPT_DIR/check_config.sh"

### Source action handlers ###

source "$SCRIPT_DIR/actions/start.sh"
source "$SCRIPT_DIR/actions/stop.sh"
source "$SCRIPT_DIR/actions/status.sh"

### Dispatch ###

restart() {
    "$SCRIPT_DIR/restart_server.sh"
}

case "$ACTION" in
    start)   start ;;
    stop)    stop ;;
    restart) restart ;;
    status)  status ;;
    *)
        log_error "Unknown action: ${ACTION:-<empty>}"
        echo "Usage: ./server.sh {start|stop|restart|status} [domains] [custom-path]"
        exit 1
        ;;
esac
