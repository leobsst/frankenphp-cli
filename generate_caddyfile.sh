#!/usr/bin/env bash

source .env
DOMAINS=$(jq -r '.domains[]' .config)
domains_list=($DOMAINS)
OS=$(uname)

if ! [ -d "caddy/sites/custom/" ]; then
    mkdir caddy/sites/custom/
fi

for domain in "${domains_list[@]}"; do
    SIMPLE_DOMAIN="${domain%.*}"
    if ! [ -f "caddy/sites/custom/${SIMPLE_DOMAIN}_Caddyfile" ]; then
        echo "Le fichier ${SIMPLE_DOMAIN}_Caddyfile n'existe pas."
        echo "Création du fichier ${SIMPLE_DOMAIN}_Caddyfile..."
        echo
        cp caddy/Caddyfile.template "caddy/sites/custom/${SIMPLE_DOMAIN}_Caddyfile"
        if [[ "$OS" == "Darwin" ]]; then
            sed -i '' "s/full_domain/${domain}/g" "caddy/sites/custom/${SIMPLE_DOMAIN}_Caddyfile"
            sed -i '' "s/custom_domain/${SIMPLE_DOMAIN}/g" "caddy/sites/custom/${SIMPLE_DOMAIN}_Caddyfile"
        else
            sed -i "s/full_domain/${domain}/g" "caddy/sites/custom/${SIMPLE_DOMAIN}_Caddyfile"
            sed -i "s/custom_domain/${SIMPLE_DOMAIN}/g" "caddy/sites/custom/${SIMPLE_DOMAIN}_Caddyfile"
        fi
    fi
done

echo
echo "-- Caddyfile configurations generated successfully --"

exit 0