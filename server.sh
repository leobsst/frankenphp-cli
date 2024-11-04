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

sudo chmod +x *.sh

### End ###


### Parameters ###

DOMAINS="${2}"
domains_list=($DOMAINS)
CUSTOM_PATH="${3:-/home}"

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

if ! [ -f check_files.sh ]; then
    echo "Il manque le fichier check_files.sh"
    exit 1
fi

sudo -u $USER ./check_files.sh
if [[ $? -ne 0 ]]; then
    echo "Il manque des fichiers."
    exit 1
fi

sudo -u $USER ./check_config.sh
if [[ $? -ne 0 ]]; then
    echo "Il manque des fichiers de configuration."
    exit 1
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

    # Ajouter les domaines au tableau "domains"
    config_domains=$(printf '%s\n' "${domains_list[@]}" | jq -R . | jq -s .)
    # Mettre à jour le JSON avec les domaines
    echo "{\"status\":\"stopped\", \"domains\":"$config_domains"}" > .config

    sudo -u $USER ./generate_ssl.sh
    if [[ $? -ne 0 ]]; then
        echo "Erreur lors de la génération des certificats SSL."
        exit 1
    fi
    for value in "${domains_list[@]}"; do
        sudo ./manage_hosts.sh add 127.0.0.1 ${value}
    done

    ./generate_caddyfile.sh
    if [[ $? -ne 0 ]]; then
        echo "Erreur lors de la génération des fichiers Caddyfile."
        exit 1
    fi

    # Mettre à jour le status en "running"
    # Mettre à jour le JSON avec le nouveau statut et les domaines
    echo "{\"status\":\"running\", \"domains\":"$config_domains"}" > .config
    echo
    echo "- Starting web server ..."

    if [[ "$APP_ENV" != "prod" ]] && [[ "$APP_ENV" != "production" ]]; then
        sudo -u $USER docker build --build-arg CUSTOM_PATH="${CUSTOM_PATH}" -t custom-frankenphp:latest . && \
            sudo -u $USER docker --log-level error compose down && \
            sudo -u $USER \
                CUSTOM_PATH=${CUSTOM_PATH} \
                PWD=$(pwd) UID=${DOCKER_USER} \
                GID=${DOCKER_GROUP} \
                DB_HOST=${DB_HOST} \
                docker --log-level error compose up -d
    else
        sudo -u $USER docker build --build-arg CUSTOM_PATH="${CUSTOM_PATH}" -t custom-frankenphp:latest . && \
        sudo -u $USER docker --log-level error compose down && \
        sudo -u $USER \
            CUSTOM_PATH=${CUSTOM_PATH} \
            PWD=$(pwd) UID=${DOCKER_USER} \
            GID=${DOCKER_GROUP} \
            DB_HOST=${DB_HOST} \
            docker --log-level error compose -f docker-compose-prod.yml up -d
    fi
}

function stop() {
    if jq -e '.status == "stopped"' .config > /dev/null; then
        echo "Le serveur est déjà arrêté."
        exit 1
    fi

    echo "- Stopping web server ..."
    sudo -u $USER docker --log-level error compose down

    DOMAINS=($(jq -r '.domains[]' .config))
    for value in "${DOMAINS[@]}"; do
        ./manage_hosts.sh remove 127.0.0.1 ${value}
    done

    sudo -u $USER ./check_config.sh reset

    echo
    echo "-- Web server stopped!"
}

### End Functions ###

$@