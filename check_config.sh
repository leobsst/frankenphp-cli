#!/usr/bin/env bash

function reset() {
    echo '{"status":"stopped", "domains":[]}' > .config
}

if ! [ -f ".config" ];
then
    echo "Aucun fichier de configuration trouvé."
    echo "Création du fichier de configuration..."
    echo
    reset
fi

# Check if status is empty
if [[ -z "$(echo "$(cat .config)" | jq '.status')" ]]; then
    reset
fi

$@