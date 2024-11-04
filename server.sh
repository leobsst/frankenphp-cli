#!/usr/bin/env bash

### Check if the script is run as root and have jq ###

if [[ "$EUID" -ne 0 ]]; then
    echo "Ce script doit être exécuté avec des droits administrateurs."
    echo "Veuillez réessayer avec 'sudo'."
    exit 1
fi

if [[ `which jq` == "" ]]; then
    echo "Requires: jq"
    exit 1
fi

### End ###



### Parameters ###

DOMAINS="${2}"
domains_list=($DOMAINS)
CUSTOM_PATH="${3:-/home}"

# Define the variables
CERTS_DIR="./caddy/certs"

if ! [ -f .env.example ]; then
    echo "Il manque le fichier .env.example"
    exit 1
fi

if ! [ -f .env ]; then
    echo "Aucun fichier .env trouvé."
    echo "Création du fichier .env..."
    echo
    cp .env.example .env
    chmod 777 .env
    echo "Veuillez définir l'utilisateur et le groupe bash dans le fichier .env"
    exit 1
fi

source .env

if [[ -z $USER ]] || [[ -z $GROUP ]]; then
    echo "Veuillez définir l'utilisateur et le groupe bash dans le fichier .env"
    exit 1
else
    chmod 770 .env
    chown $USER:$GROUP .env
fi

if [[ -z $DOCKER_USER ]] || [[ -z $DOCKER_GROUP ]]; then
    echo "Veuillez définir l'utilisateur et le groupe docker dans le fichier .env"
    exit 1
fi

### End Parameters ###



### Check if required files exist ###

if ! [ -f manage_hosts.sh ]; then
    echo "Il manque le fichier manage_host.sh"
    exit 1
else
    chmod +x manage_hosts.sh
fi

if ! [ -f caddy/Caddyfile ]; then
    echo "Il manque le fichier caddy/Caddyfile"
    exit 1
fi

if ! [ -f Dockerfile ]; then
    echo "Il manque le fichier Dockerfile"
    exit 1
fi

if ! [ -f docker-compose.yml ]; then
    echo "Il manque le fichier docker-compose.yml"
    exit 1
fi

if ! [ -f php/php.ini ]; then
    echo "Il manque le fichier php/php.ini"
    exit 1
fi

if ! [ -d $CERTS_DIR ]; then
    mkdir -p $CERTS_DIR
fi

reset-config() {
    echo '{"status":"stopped", "domains":[]}' > .config
}

if ! [ -f ".config" ];
then
    echo "Aucun fichier de configuration trouvé."
    echo "Création du fichier de configuration..."
    echo
    reset-config
fi

# Check if status is empty
if [[ -z "$(echo "$(cat .config)" | jq '.status')" ]]; then
    reset-config
fi

### End ###



### Functions ###

function start() {
    if jq -e '.status == "running"' .config > /dev/null; then
        echo "Le serveur est déjà en cours d'exécution."
        exit 1
    fi

    if [[ -z $DOMAINS ]]; then
        echo "Please provide required parameters: sudo ./server.sh start <domain.s> <custom-path>"
        exit 1
    fi

    # first ensure required executables exists:
    if [[ "$APP_ENV" != "prod" ]] && [[ "$APP_ENV" != "production" ]]; then
        if [[ `which mkcert` == "" ]]; then
            echo "Requires: mkcert & nss"
            exit 1
        fi

        # finally install certificates
        echo "-- Installing mkcert ..."
        sudo -u $USER mkcert -install
        echo "-- Creating and installing local SSL certificates for domain.s: ${DOMAINS} ..."

        sudo -u $USER mkcert -cert-file ${CERTS_DIR}/localhost.pem -key-file ${CERTS_DIR}/localhost-key.pem localhost

        for value in "${domains_list[@]}"; do
            CERT_PEM_FILE="${CERTS_DIR}/${value}.pem"
            KEY_PEM_FILE="${CERTS_DIR}/${value}-key.pem"

            sudo -u $USER mkcert -cert-file ${CERT_PEM_FILE} -key-file ${KEY_PEM_FILE} ${value}

            sudo ./manage_hosts.sh add 127.0.0.1 ${value}
        done

        echo
        echo "-- New SSL certificates generated!"
    else
        for value in "${domains_list[@]}"; do
            sudo ./manage_hosts.sh add 127.0.0.1 ${value}
        done
    fi

    # Mettre à jour le status en "running"
    # Ajouter les domaines au tableau "domains"
    config_domains=$(printf '%s\n' "${domains_list[@]}" | jq -R . | jq -s .)
    # Mettre à jour le JSON avec le nouveau statut et les domaines
    echo "{\"status\":\"running\", \"domains\":"$config_domains"}" > .config
    echo
    echo "- Starting web server ..."

    if [[ "$APP_ENV" != "prod" ]] && [[ "$APP_ENV" != "production" ]]; then
        sudo -u $USER docker build --build-arg CUSTOM_PATH="${CUSTOM_PATH}" -t custom-frankenphp:latest . && \
            sudo -u $USER docker compose down && \
            sudo -u $USER CUSTOM_PATH=${CUSTOM_PATH} PWD=$(pwd) UID=${DOCKER_USER} GID=${DOCKER_GROUP} DB_HOST=${DB_HOST} docker-compose up -d
    else
        sudo -u $USER docker build --build-arg CUSTOM_PATH="${CUSTOM_PATH}" -t custom-frankenphp:latest . && \
        sudo -u $USER docker compose down && \
        sudo -u $USER CUSTOM_PATH=${CUSTOM_PATH} PWD=$(pwd) UID=${DOCKER_USER} GID=${DOCKER_GROUP} DB_HOST=${DB_HOST} docker-compose -f docker-compose-prod.yml up -d
    fi

    # for value in "${domains_list[@]}"; do
    #     DOMAIN=${value%.*}
    #     if [ "$DOMAIN" = "mediplace" ]
    #     then
    #         cp -n $(pwd)/caddy/Caddyfile.template $(pwd)/caddy/mediplace_Caddyfile
    #         docker run -d \
    #         -v $(pwd)/caddy/mediplace_Caddyfile:/etc/caddy/Caddyfile \
    #         -v $(pwd)/caddy/certs:/certs \
    #         -v $(pwd)/caddy/data:/data \
    #         -v $(pwd)/caddy/config:/config \
    #         -v $(pwd)/caddy/logs/${DOMAIN}:/var/log \
    #         -v $(pwd)/php/php.ini:/usr/local/etc/php/php.ini \
    #         -v $CUSTOM_PATH/api-mediprix:/$CUSTOM_PATH/api-mediprix \
    #         -p 80:80 \
    #         -p 443:443 \
    #         -p 443:443/udp \
    #         -e DOMAIN=${value} \
    #         -e PROJECT=api-mediprix \
    #         --name ${DOMAIN} \
    #         --user 501:20 \
    #         --restart unless-stopped \
    #         custom-frankenphp:latest
    #         ((ITER++))
    #     else
    #         cp -n $(pwd)/caddy/Caddyfile.template $(pwd)/caddy/${DOMAIN}_Caddyfile
    #         docker run -d \
    #         -v $(pwd)/caddy/${DOMAIN}_Caddyfile:/etc/caddy/Caddyfile \
    #         -v $(pwd)/caddy/certs:/certs \
    #         -v $(pwd)/caddy/data:/data \
    #         -v $(pwd)/caddy/config:/config \
    #         -v $(pwd)/caddy/logs/${DOMAIN}:/var/log \
    #         -v $(pwd)/php/php.ini:/usr/local/etc/php/php.ini \
    #         -v $CUSTOM_PATH/$DOMAIN:/$CUSTOM_PATH/${DOMAIN} \
    #         -p 80:80 \
    #         -p 443:443 \
    #         -p 443:443/udp \
    #         -e DOMAIN=${value} \
    #         -e PROJECT=${DOMAIN} \
    #         --name ${DOMAIN} \
    #         --user 501:20 \
    #         --restart unless-stopped \
    #         custom-frankenphp:latest
    #         ((ITER++))
    #     fi
    # done


    # docker run -d \
    #     -v $(pwd)/caddy/Caddyfile:/etc/caddy/Caddyfile \
    #     -v $(pwd)/caddy/certs:/certs \
    #     -v $(pwd)/caddy/data:/data \
    #     -v $(pwd)/caddy/config:/config \
    #     -v $(pwd)/caddy/log:/var/log \
    #     -v $(pwd)/php/php.ini:/usr/local/etc/php/php.ini \
    #     -v $CUSTOM_PATH/api-mediprix:/$CUSTOM_PATH/api-mediprix \
    #     -v $CUSTOM_PATH/mediprix-v2:/$CUSTOM_PATH/mediprix-v2 \
    #     -v $CUSTOM_PATH/fluxdeflammes:/$CUSTOM_PATH/fluxdeflammes \
    #     -p 80:80 \
    #     -p 443:443 \
    #     -p 443:443/udp \
    #     --name franken-php \
    #     --user 501:20 \
    #     --restart unless-stopped \
    #     --net=host \
    #     custom-frankenphp:latest
}

function stop() {
    if jq -e '.status == "stopped"' .config > /dev/null; then
        echo "Le serveur est déjà arrêté."
        exit 1
    fi

    echo "- Stopping web server ..."
    sudo -u $USER docker compose down

    DOMAINS=($(jq -r '.domains[]' .config))
    for value in "${DOMAINS[@]}"; do
        sudo ./manage_hosts.sh remove 127.0.0.1 ${value}
    done

    reset-config

    echo
    echo "-- Web server stopped!"
}

### End Functions ###

$@