#!/usr/bin/env bash

# Shared utility functions for all scripts
# Source this file at the top of each script: source "$(dirname "$0")/utils.sh"

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- OS Detection ---

OS="$(uname)"

is_macos() {
    [[ "$OS" == "Darwin" ]]
}

# Platform-aware sed in-place edit
sed_inplace() {
    if is_macos; then
        sed -i '' "$@"
    else
        sed -i "$@"
    fi
}

# --- Logging ---

log_info() {
    echo "-- $*"
}

log_error() {
    echo "ERROR: $*" >&2
}

log_success() {
    echo "-- $*"
}

# --- Validation ---

require_command() {
    local cmd="$1"
    if ! command -v "$cmd" &>/dev/null; then
        log_error "Required command not found: $cmd"
        exit 1
    fi
}

require_file() {
    local file="$1"
    if [[ ! -f "$file" ]]; then
        log_error "Missing file: $file"
        exit 1
    fi
}

require_directory() {
    local dir="$1"
    if [[ ! -d "$dir" ]]; then
        log_error "Directory does not exist: $dir"
        exit 1
    fi
}

require_env_var() {
    local var_name="$1"
    if [[ -z "${!var_name:-}" ]]; then
        log_error "Environment variable '$var_name' is not set."
        exit 1
    fi
}

# Validate a domain name format (e.g. myapp.test)
validate_domain() {
    local domain="$1"
    if [[ ! "$domain" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z]{2,})+$ ]]; then
        log_error "Invalid domain name: $domain"
        return 1
    fi
}

# --- Environment ---

is_production() {
    [[ "${APP_ENV:-dev}" == "prod" || "${APP_ENV:-dev}" == "production" ]]
}

# Load .env file if it exists
load_env() {
    local env_file="${1:-$SCRIPT_DIR/.env}"
    if [[ -f "$env_file" ]]; then
        set -a
        # shellcheck source=/dev/null
        source "$env_file"
        set +a
    else
        log_error "No .env file found: $env_file"
        exit 1
    fi
}

# Load .config file and expose domains
load_config() {
    local config_file="$SCRIPT_DIR/.config"
    require_file "$config_file"
    require_command jq
}

get_config_status() {
    jq -r '.status' "$SCRIPT_DIR/.config"
}

get_config_domains() {
    jq -r '.domains[]' "$SCRIPT_DIR/.config"
}

# Password history file — stores all previously used passwords (one per line)
# so we can always authenticate to MariaDB even after .env changes.
DB_PASSWORD_HISTORY="$SCRIPT_DIR/.db_password_history"

# Add a password to the history file (avoids duplicates)
save_password_to_history() {
    local password="$1"
    local history_file="$DB_PASSWORD_HISTORY"

    if [[ ! -f "$history_file" ]]; then
        touch "$history_file"
        chmod 660 "$history_file"
        chown "${USER:-root}:${GROUP:-root}" "$history_file" 2>/dev/null || true
    fi

    if ! grep -qxF "$password" "$history_file" 2>/dev/null; then
        echo "$password" >> "$history_file"
    fi
}

# Try to authenticate to MariaDB using the new password first,
# then fall back to all passwords in history.
# Returns the working password via stdout, or empty string on failure.
find_working_password() {
    local new_password="$1"
    local container="franken_mariadb"

    # Try new password first
    if docker exec "$container" mariadb -u root -p"$new_password" -e "SELECT 1" &>/dev/null; then
        echo "$new_password"
        return 0
    fi

    # Try all passwords from history (newest first)
    if [[ -f "$DB_PASSWORD_HISTORY" ]]; then
        while IFS= read -r old_password; do
            [[ -z "$old_password" ]] && continue
            if docker exec "$container" mariadb -u root -p"$old_password" -e "SELECT 1" &>/dev/null; then
                echo "$old_password"
                return 0
            fi
        done < <(if is_macos; then tail -r "$DB_PASSWORD_HISTORY"; else tac "$DB_PASSWORD_HISTORY"; fi)
    fi

    return 1
}

# Sync the .env password into the running MariaDB instance via ALTER USER.
# Uses password history to authenticate if the current password was changed in .env.
sync_db_password() {
    local new_password="$1"
    local container="franken_mariadb"

    # Wait for MariaDB to be ready
    local retries=30
    local auth_password=""
    while [[ -z "$auth_password" ]]; do
        auth_password="$(find_working_password "$new_password")" && break
        retries=$((retries - 1))
        if [[ "$retries" -le 0 ]]; then
            log_info "MariaDB password sync skipped (could not connect)."
            return 0
        fi
        sleep 1
    done

    # Already in sync
    if [[ "$auth_password" == "$new_password" ]]; then
        save_password_to_history "$new_password"
        log_info "MariaDB password already in sync."
        return 0
    fi

    # Apply new password using old one to authenticate
    if docker exec "$container" mariadb -u root -p"$auth_password" -e \
        "ALTER USER 'root'@'%' IDENTIFIED BY '$new_password'; ALTER USER 'root'@'localhost' IDENTIFIED BY '$new_password'; FLUSH PRIVILEGES;" &>/dev/null; then
        save_password_to_history "$new_password"
        log_success "MariaDB password updated to match .env"
    else
        log_error "Failed to sync MariaDB password."
    fi
}
