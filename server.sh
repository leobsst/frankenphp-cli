#!/usr/bin/env bash

source "$(dirname "$0")/utils.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

### Check if the script is run as root (except for restart) ###

ACTION="$(echo "${1:-}" | tr '[:upper:]' '[:lower:]')"

if [[ "$EUID" -ne 0 ]] && [[ "$ACTION" != "restart" ]]; then
    log_error "Ce script doit être exécuté avec des droits administrateurs."
    echo "Veuillez réessayer avec 'sudo'."
    exit 1
fi

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
    echo "Aucun fichier .env trouvé."
    echo "Création du fichier .env..."
    echo
    sudo -u "$USER" cp "$SCRIPT_DIR/.env.example" "$SCRIPT_DIR/.env"
    chmod 660 "$SCRIPT_DIR/.env"
    echo "Veuillez définir l'utilisateur et le groupe bash dans le fichier .env"
    exit 1
fi

load_env "$SCRIPT_DIR/.env"

require_env_var USER
require_env_var GROUP

# Generate MariaDB root password if not set
if [[ -z "${MARIADB_ROOT_PASSWORD:-}" ]]; then
    MARIADB_ROOT_PASSWORD="$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)"
    echo "MARIADB_ROOT_PASSWORD=$MARIADB_ROOT_PASSWORD" >> "$SCRIPT_DIR/.env"
    log_info "Mot de passe MariaDB généré et ajouté au fichier .env"
fi

# Compute port bindings based on EXPOSE_SERVICES
if [[ "${EXPOSE_SERVICES:-false}" == "true" ]]; then
    export DB_PORT="3306:3306"
    export PMA_PORT="8080:80"
    export REDIS_PORT="6379:6379"
else
    export DB_PORT="127.0.0.1:3306:3306"
    export PMA_PORT="127.0.0.1:8080:80"
    export REDIS_PORT="127.0.0.1:6379:6379"
fi

chmod 770 "$SCRIPT_DIR/.env"
chown "$USER:$GROUP" "$SCRIPT_DIR/.env"

### Check if required files exist ###

require_file "$SCRIPT_DIR/check_files.sh"

sudo -u "$USER" "$SCRIPT_DIR/check_files.sh"
sudo -u "$USER" "$SCRIPT_DIR/check_config.sh"

### Cleanup trap for start — rollback /etc/hosts if something fails ###

hosts_added=()

cleanup_on_failure() {
    log_error "Une erreur est survenue. Nettoyage en cours..."
    for domain in "${hosts_added[@]}"; do
        "$SCRIPT_DIR/manage_hosts.sh" remove 127.0.0.1 "$domain" 2>/dev/null || true
    done
    # Reset config to stopped
    echo '{"status":"stopped", "domains":[]}' > "$SCRIPT_DIR/.config"
    log_error "Nettoyage terminé. Le serveur n'a pas démarré."
    exit 1
}

### Functions ###

start() {
    if jq -e '.status == "running"' "$SCRIPT_DIR/.config" > /dev/null 2>&1; then
        log_error "Le serveur est déjà en cours d'exécution."
        exit 1
    fi

    if [[ -z "$DOMAINS" ]]; then
        log_error "Paramètres manquants."
        echo "Usage: sudo ./server.sh start <domain.s> <custom-path>"
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

    sudo -u "$USER" "$SCRIPT_DIR/generate_ssl.sh"

    for domain in "${domains_list[@]}"; do
        sudo "$SCRIPT_DIR/manage_hosts.sh" add 127.0.0.1 "$domain"
        hosts_added+=("$domain")
    done

    "$SCRIPT_DIR/generate_caddyfile.sh"

    echo "{\"status\":\"running\", \"domains\":$config_domains}" > "$SCRIPT_DIR/.config"

    echo
    log_info "Starting web server ..."

    sudo -u "$USER" docker build \
        --build-arg CUSTOM_PATH="$CUSTOM_PATH" \
        --build-arg WWWGROUP="${WWWGROUP:-}" \
        -t custom-frankenphp:latest "$SCRIPT_DIR"

    sudo -u "$USER" docker --log-level error compose down 2>/dev/null || true

    if is_production; then
        sudo -u "$USER" \
            CUSTOM_PATH="$CUSTOM_PATH" \
            DB_PORT="$DB_PORT" \
            PMA_PORT="$PMA_PORT" \
            REDIS_PORT="$REDIS_PORT" \
            PWD="$SCRIPT_DIR" \
            docker --log-level error compose -f docker-compose-prod.yml up -d
    else
        sudo -u "$USER" \
            CUSTOM_PATH="$CUSTOM_PATH" \
            DB_PORT="$DB_PORT" \
            PMA_PORT="$PMA_PORT" \
            REDIS_PORT="$REDIS_PORT" \
            PWD="$SCRIPT_DIR" \
            docker --log-level error compose up -d
    fi

    # Disable trap after success
    trap - ERR

    echo
    log_success "Web server started!"
}

stop() {
    if jq -e '.status == "stopped"' "$SCRIPT_DIR/.config" > /dev/null 2>&1; then
        log_error "Le serveur est déjà arrêté."
        exit 1
    fi

    log_info "Stopping web server ..."

    if is_production; then
        sudo -u "$USER" docker --log-level error compose -f docker-compose-prod.yml down
    else
        sudo -u "$USER" docker --log-level error compose down
    fi

    local config_domains=()
    while IFS= read -r d; do
        config_domains+=("$d")
    done < <(jq -r '.domains[]' "$SCRIPT_DIR/.config")
    for domain in "${config_domains[@]}"; do
        "$SCRIPT_DIR/manage_hosts.sh" remove 127.0.0.1 "$domain"
    done

    sudo -u "$USER" "$SCRIPT_DIR/check_config.sh" reset

    echo
    log_success "Web server stopped!"
}

restart() {
    sudo -u "$USER" "$SCRIPT_DIR/restart_server.sh"
}

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

### Dispatch ###

case "$ACTION" in
    start)   start ;;
    stop)    stop ;;
    restart) restart ;;
    status)  status ;;
    *)
        log_error "Action inconnue: ${ACTION:-<vide>}"
        echo "Usage: sudo ./server.sh {start|stop|restart|status} [domains] [custom-path]"
        exit 1
        ;;
esac
