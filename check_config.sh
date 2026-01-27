#!/usr/bin/env bash

source "$(dirname "$0")/utils.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="$SCRIPT_DIR/.config"

reset() {
    echo '{"status":"stopped", "domains":[]}' > "$CONFIG_FILE"
}

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "Aucun fichier de configuration trouvé."
    echo "Création du fichier de configuration..."
    echo
    reset
fi

# Check if status is empty or invalid
if ! jq -e '.status' "$CONFIG_FILE" > /dev/null 2>&1; then
    reset
fi

### Dispatch ###

case "${1:-}" in
    reset) reset ;;
    "")    ;; # No action, just validation above
    *)
        log_error "Action inconnue: $1"
        echo "Usage: ./check_config.sh [reset]"
        exit 1
        ;;
esac
