#!/usr/bin/env bash

source "$(dirname "$0")/utils.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

load_env "$SCRIPT_DIR/.env"

require_command jq
require_command mkcert

domains_list=()
while IFS= read -r d; do
    domains_list+=("$d")
done < <(get_config_domains)

MKCERT="$(command -v mkcert)"

# mkcert -install requires sudo to add root CA to system trust store
log_info "Installing mkcert ..."
sudo "$MKCERT" -install

"$MKCERT" -cert-file "$CERTS_DIR/localhost.pem" -key-file "$CERTS_DIR/localhost-key.pem" localhost

if ! is_production; then
    log_info "Creating and installing local SSL certificates for domain(s): ${domains_list[*]} ..."

    for domain in "${domains_list[@]}"; do
        CERT_PEM_FILE="$CERTS_DIR/${domain}.pem"
        KEY_PEM_FILE="$CERTS_DIR/${domain}-key.pem"

        # Skip if certificate already exists and is less than 30 days old
        if [[ "${FORCE_SSL:-0}" != "1" ]] && [[ -f "$CERT_PEM_FILE" ]] && [[ -f "$KEY_PEM_FILE" ]]; then
            if is_macos; then
                cert_age=$(( ( $(date +%s) - $(stat -f %m "$CERT_PEM_FILE") ) / 86400 ))
            else
                cert_age=$(( ( $(date +%s) - $(date -r "$CERT_PEM_FILE" +%s) ) / 86400 ))
            fi

            if [[ "$cert_age" -lt 30 ]]; then
                echo "  Certificate for $domain is still valid ($cert_age days old), skipping."
                continue
            fi
        fi

        "$MKCERT" -cert-file "$CERT_PEM_FILE" -key-file "$KEY_PEM_FILE" "$domain"
    done

    echo
    log_success "New SSL certificates generated!"
fi

chmod -R 750 "$CERTS_DIR/"

exit 0
