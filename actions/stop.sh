#!/usr/bin/env bash

# Stop action handler for server.sh
# This file is sourced by server.sh and has access to all its variables.

stop() {
    if jq -e '.status == "stopped"' "$SCRIPT_DIR/.config" > /dev/null 2>&1; then
        log_error "The server is already stopped."
        exit 1
    fi

    log_info "Stopping web server ..."

    if is_production; then
        docker --log-level error compose -f docker-compose-prod.yml down
    else
        docker --log-level error compose down
    fi

    local config_domains=()
    while IFS= read -r d; do
        config_domains+=("$d")
    done < <(jq -r '.domains[]' "$SCRIPT_DIR/.config")

    # Modifying /etc/hosts requires sudo
    for domain in "${config_domains[@]}"; do
        sudo "$SCRIPT_DIR/manage_hosts.sh" remove 127.0.0.1 "$domain"
    done

    "$SCRIPT_DIR/check_config.sh" reset

    echo
    log_success "Web server stopped!"
}
