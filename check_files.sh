#!/usr/bin/env bash

source "$(dirname "$0")/utils.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

load_env "$SCRIPT_DIR/.env"

SHELL_FILES=(
    manage_hosts.sh
    check_config.sh
    generate_ssl.sh
    generate_caddyfile.sh
    restart_server.sh
    utils.sh
    actions/start.sh
    actions/stop.sh
    actions/status.sh
)

APP_FILES=(
    caddy/Caddyfile
    Dockerfile
    docker-compose.yml
    docker-compose-prod.yml
    php/php.ini
    php/php-prod.ini
)

for shell_file in "${SHELL_FILES[@]}"; do
    require_file "$SCRIPT_DIR/$shell_file"
    chmod +x "$SCRIPT_DIR/$shell_file"
done

for app_file in "${APP_FILES[@]}"; do
    require_file "$SCRIPT_DIR/$app_file"
done

if [[ ! -d "$CERTS_DIR" ]]; then
    mkdir -p "$CERTS_DIR"
fi

exit 0
