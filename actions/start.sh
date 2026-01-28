#!/usr/bin/env bash

# Start action handler for server.sh
# This file is sourced by server.sh and has access to all its variables.

hosts_added=()

cleanup_on_failure() {
    log_error "An error occurred. Cleaning up..."
    for domain in "${hosts_added[@]}"; do
        sudo "$SCRIPT_DIR/manage_hosts.sh" remove 127.0.0.1 "$domain" 2>/dev/null || true
    done
    # Reset config to stopped
    echo '{"status":"stopped", "domains":[]}' > "$SCRIPT_DIR/.config"
    log_error "Cleanup complete. The server did not start."
    exit 1
}

start() {
    if jq -e '.status == "running"' "$SCRIPT_DIR/.config" > /dev/null 2>&1; then
        log_error "The server is already running."
        exit 1
    fi

    if [[ -z "$DOMAINS" ]]; then
        log_error "Missing parameters."
        echo "Usage: ./server.sh start <domain.s> <custom-path>"
        exit 1
    fi

    # Validate custom path exists
    require_directory "$CUSTOM_PATH"

    # Validate domain names
    for domain in "${domains_list[@]}"; do
        validate_domain "$domain"
    done

    # Remove duplicates (preserves order)
    local deduped
    deduped=$(printf '%s\n' "${domains_list[@]}" | awk '!seen[$0]++')
    domains_list=()
    while IFS= read -r d; do
        domains_list+=("$d")
    done <<< "$deduped"

    # Write config with domains
    local config_domains
    config_domains=$(printf '%s\n' "${domains_list[@]}" | jq -R . | jq -s .)
    echo "{\"status\":\"stopped\", \"domains\":$config_domains}" > "$SCRIPT_DIR/.config"

    # Enable cleanup trap from this point
    trap cleanup_on_failure ERR

    "$SCRIPT_DIR/generate_ssl.sh"

    # Modifying /etc/hosts requires sudo
    for domain in "${domains_list[@]}"; do
        sudo "$SCRIPT_DIR/manage_hosts.sh" add 127.0.0.1 "$domain"
        hosts_added+=("$domain")
    done

    "$SCRIPT_DIR/generate_caddyfile.sh"

    echo "{\"status\":\"running\", \"domains\":$config_domains}" > "$SCRIPT_DIR/.config"

    echo
    log_info "Starting web server ..."

    docker build \
        --build-arg CUSTOM_PATH="$CUSTOM_PATH" \
        --build-arg WWWGROUP="${WWWGROUP:-}" \
        -t custom-frankenphp:latest "$SCRIPT_DIR"

    docker --log-level error compose down 2>/dev/null || true

    if is_production; then
        CUSTOM_PATH="$CUSTOM_PATH" \
        DB_PORT="$DB_PORT" \
        PMA_PORT="$PMA_PORT" \
        REDIS_PORT="$REDIS_PORT" \
        MARIADB_ROOT_PASSWORD="$MARIADB_ROOT_PASSWORD" \
        PWD="$SCRIPT_DIR" \
        docker --log-level error compose -f docker-compose-prod.yml up -d
    else
        CUSTOM_PATH="$CUSTOM_PATH" \
        DB_PORT="$DB_PORT" \
        PMA_PORT="$PMA_PORT" \
        REDIS_PORT="$REDIS_PORT" \
        MARIADB_ROOT_PASSWORD="$MARIADB_ROOT_PASSWORD" \
        PWD="$SCRIPT_DIR" \
        docker --log-level error compose up -d
    fi

    # Disable trap after success
    trap - ERR

    # Sync MariaDB password with .env value
    sync_db_password "$MARIADB_ROOT_PASSWORD"

    echo
    log_success "Web server started!"
}
