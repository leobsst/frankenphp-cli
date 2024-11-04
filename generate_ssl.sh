#!/usr/bin/env bash

source .env
DOMAINS=$(jq -r '.domains[]' .config)
domains_list=($DOMAINS)

if [[ `which mkcert` == "" ]]; then
    echo "Requires: mkcert & nss"
    exit 1
fi

# finally install certificates
echo "-- Installing mkcert ..."
sudo -u $USER mkcert -install
sudo -u $USER mkcert -cert-file $CERTS_DIR/localhost.pem -key-file $CERTS_DIR/localhost-key.pem localhost

if [[ "$APP_ENV" != "prod" ]] && [[ "$APP_ENV" != "production" ]]; then
    echo "-- Creating and installing local SSL certificates for domain.s: ${DOMAINS} ..."

    sudo -u $USER mkcert -cert-file $CERTS_DIR/localhost.pem -key-file $CERTS_DIR/localhost-key.pem localhost

    for value in "${domains_list[@]}"; do
        CERT_PEM_FILE="$CERTS_DIR/${value}.pem"
        KEY_PEM_FILE="$CERTS_DIR/${value}-key.pem"

        sudo -u $USER mkcert -cert-file ${CERT_PEM_FILE} -key-file ${KEY_PEM_FILE} ${value}
    done

    echo
    echo "-- New SSL certificates generated!"
fi

exit 0