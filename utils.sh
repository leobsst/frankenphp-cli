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
    echo "ERREUR: $*" >&2
}

log_success() {
    echo "-- $*"
}

# --- Validation ---

require_command() {
    local cmd="$1"
    if ! command -v "$cmd" &>/dev/null; then
        log_error "Commande requise introuvable: $cmd"
        exit 1
    fi
}

require_file() {
    local file="$1"
    if [[ ! -f "$file" ]]; then
        log_error "Il manque le fichier: $file"
        exit 1
    fi
}

require_directory() {
    local dir="$1"
    if [[ ! -d "$dir" ]]; then
        log_error "Le répertoire n'existe pas: $dir"
        exit 1
    fi
}

require_env_var() {
    local var_name="$1"
    if [[ -z "${!var_name:-}" ]]; then
        log_error "La variable d'environnement '$var_name' n'est pas définie."
        exit 1
    fi
}

# Validate a domain name format (e.g. myapp.test)
validate_domain() {
    local domain="$1"
    if [[ ! "$domain" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z]{2,})+$ ]]; then
        log_error "Nom de domaine invalide: $domain"
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
        source "$env_file"
        set +a
    else
        log_error "Aucun fichier .env trouvé: $env_file"
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
