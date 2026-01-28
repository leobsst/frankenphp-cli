#!/usr/bin/env bash

# Status action handler for server.sh
# This file is sourced by server.sh and has access to all its variables.

status() {
    local current_status
    current_status=$(jq -r '.status' "$SCRIPT_DIR/.config" 2>/dev/null || echo "unknown")

    echo "=== FrankenPHP Server Status ==="
    echo
    echo "Status: $current_status"

    if [[ "$current_status" == "running" ]]; then
        echo
        echo "Domains:"
        jq -r '.domains[]' "$SCRIPT_DIR/.config" 2>/dev/null | while IFS= read -r d; do
            echo "  - $d"
        done

        echo
        echo "Containers:"
        local containers=("webserver-and-caddy" "franken_mariadb" "franken_phpmyadmin" "franken_redis")
        for container in "${containers[@]}"; do
            local state
            state=$(docker inspect -f '{{.State.Status}} ({{.State.Health.Status}})' "$container" 2>/dev/null || echo "not found")
            # Clean up if no healthcheck
            state=$(echo "$state" | sed 's/ ()//; s/ (<nil>)//')
            printf "  %-25s %s\n" "$container" "$state"
        done
    fi

    echo
}
