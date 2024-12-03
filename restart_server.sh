#!/usr/bin/env bash

source .env

if [[ `which jq` == "" ]]; then
    echo "Requires: jq"
    exit 1
fi

if ! [ -f .env ]; then
    echo "Aucun fichier .env trouvé."
    echo "Veuillez exécuter le script server.sh"
    exit 1
fi

if [[ -z $USER ]] || [[ -z $GROUP ]]; then
    echo "Veuillez définir l'utilisateur et le groupe bash dans le fichier .env"
    exit 1
fi

if [[ -z $DOCKER_USER ]] || [[ -z $DOCKER_GROUP ]]; then
    echo "Veuillez exécuter le script server.sh"
    exit 1
fi

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

if jq -e '.status == "stopped"' .config > /dev/null; then
    echo "Le serveur n'est pas en cours d'exécution."
    exit 1
fi

echo
echo "-- Restarting webserver!"

echo
echo "-- Generating new SSL certificates!"
echo
./generate_ssl.sh \
    && sudo -u $USER docker restart webserver-and-caddy >> /dev/null 2>&1 \
    && sudo -u $USER docker restart mariadb >> /dev/null 2>&1 \
    && sudo -u $USER docker restart phpmyadmin >> /dev/null 2>&1 \
    && sudo -u $USER docker restart redis >> /dev/null 2>&1

echo
echo "-- Web server restarted! -- ✅"