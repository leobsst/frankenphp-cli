#!/usr/bin/env bash

source "$(dirname "$0")/utils.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

load_env "$SCRIPT_DIR/.env"

require_command jq

domains_list=()
while IFS= read -r d; do
    domains_list+=("$d")
done < <(get_config_domains)

CUSTOM_DIR="$SCRIPT_DIR/caddy/sites/custom"

if [[ ! -d "$CUSTOM_DIR" ]]; then
    mkdir -p "$CUSTOM_DIR"
    chmod 750 "$CUSTOM_DIR"
fi

for domain in "${domains_list[@]}"; do
    SIMPLE_DOMAIN="${domain%.*}"
    CADDYFILE="$CUSTOM_DIR/${SIMPLE_DOMAIN}_Caddyfile"

    if [[ ! -f "$CADDYFILE" ]]; then
        echo "Caddyfile ${SIMPLE_DOMAIN}_Caddyfile does not exist."
        echo "Creating ${SIMPLE_DOMAIN}_Caddyfile..."
        echo

        cp "$SCRIPT_DIR/caddy/Caddyfile.template" "$CADDYFILE"
        sed_inplace "s/full_domain/${domain}/g" "$CADDYFILE"
        sed_inplace "s/custom_domain/${SIMPLE_DOMAIN}/g" "$CADDYFILE"
    fi
done

echo
log_success "Caddyfile configurations generated successfully"

exit 0
