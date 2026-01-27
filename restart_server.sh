#!/usr/bin/env bash

source "$(dirname "$0")/utils.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Parse optional flags
FORCE_SSL="${FORCE_SSL:-0}"
export FORCE_SSL
for arg in "$@"; do
    case "$arg" in
        --force-ssl) FORCE_SSL=1 ;;
    esac
done

load_env "$SCRIPT_DIR/.env"

require_command jq

require_file "$SCRIPT_DIR/.env"
require_env_var USER
require_env_var GROUP

# Generate MariaDB root password if not set
if [[ -z "${MARIADB_ROOT_PASSWORD:-}" ]]; then
    MARIADB_ROOT_PASSWORD="$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)"
    echo "MARIADB_ROOT_PASSWORD=$MARIADB_ROOT_PASSWORD" >> "$SCRIPT_DIR/.env"
    log_info "Mot de passe MariaDB généré et ajouté au fichier .env"
fi

require_file "$SCRIPT_DIR/check_files.sh"

sudo -u "$USER" "$SCRIPT_DIR/check_files.sh"
sudo -u "$USER" "$SCRIPT_DIR/check_config.sh"

if jq -e '.status == "stopped"' "$SCRIPT_DIR/.config" > /dev/null 2>&1; then
    log_error "Le serveur n'est pas en cours d'exécution."
    exit 1
fi

echo
log_info "Restarting webserver!"

echo
log_info "Generating SSL certificates!"
echo
"$SCRIPT_DIR/generate_ssl.sh"

sudo -u "$USER" docker restart webserver-and-caddy > /dev/null 2>&1
sudo -u "$USER" docker restart franken_mariadb > /dev/null 2>&1
sudo -u "$USER" docker restart franken_phpmyadmin > /dev/null 2>&1
sudo -u "$USER" docker restart franken_redis > /dev/null 2>&1

echo
log_success "Web server restarted!"
