#!/bin/sh
set -e

# Trust the local mkcert CA inside the container so outgoing PHP/curl
# calls to other .test/.local domains (e.g. cross-project API calls)
# validate the same certificates the host browser already trusts.
if [ -f /certs/rootCA.pem ]; then
    cp /certs/rootCA.pem /usr/local/share/ca-certificates/mkcert-rootCA.crt
    update-ca-certificates
fi

exec docker-php-entrypoint "$@"
