#!/usr/bin/env bash

source .env
SHELL_FILES_TO_CHECK='manage_hosts.sh check_config.sh generate_ssl.sh generate_caddyfile.sh'
SHELL_FILES_TO_CHECK=($SHELL_FILES_TO_CHECK)
APP_FILES_TO_CHECK=('caddy/Caddyfile Dockerfile docker-compose.yml docker-compose-prod.yml php/php.ini')
APP_FILES_TO_CHECK=($APP_FILES_TO_CHECK)

for shell in "${SHELL_FILES_TO_CHECK[@]}"; do
    if ! [[ -f ${shell} ]]; then
        echo "Il manque le fichier ${shell}"
        exit 1
    else
        chmod +x ${shell}
    fi
done

for file in "${APP_FILES_TO_CHECK[@]}"; do
    if ! [[ -f ${file} ]]; then
        echo "Il manque le fichier ${file}"
        exit 1
    fi
done

if ! [ -d $CERTS_DIR ]; then
    mkdir -p $CERTS_DIR
fi

exit 0