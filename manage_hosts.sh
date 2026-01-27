#!/usr/bin/env bash

source "$(dirname "$0")/utils.sh"

ETC_HOSTS=/etc/hosts

remove() {
    local IP="${1:-127.0.0.1}"
    local HOSTNAME="${2:-localhost}"
    local HOSTS_LINE="$IP[[:space:]]$HOSTNAME"
    local HOSTS_LINE_LOCAL="::1[[:space:]]*$HOSTNAME"

    if [[ "$HOSTNAME" == "localhost" ]]; then
        return
    fi

    if grep -qE "$HOSTS_LINE" "$ETC_HOSTS"; then
        echo "$HOSTS_LINE Found in your $ETC_HOSTS, Removing now..."
        sed_inplace "/$HOSTS_LINE/d" "$ETC_HOSTS"

        if [[ "$IP" == "127.0.0.1" ]]; then
            if grep -qE "::1[[:space:]]+$HOSTNAME" "$ETC_HOSTS"; then
                sed_inplace "/$HOSTS_LINE_LOCAL/d" "$ETC_HOSTS"
            fi
        fi
    else
        echo "$HOSTS_LINE was not found in your $ETC_HOSTS"
    fi
}

add() {
    local IP="${1:-127.0.0.1}"
    local HOSTNAME="${2:-localhost}"
    local HOSTS_LINE="$IP[[:space:]]$HOSTNAME"
    local line_content
    local line_content_local

    line_content=$(printf "%s\t%s\n" "$IP" "$HOSTNAME")
    line_content_local=$(printf "%s\t\t%s\n" "::1" "$HOSTNAME")

    if grep -qE "$HOSTS_LINE" "$ETC_HOSTS"; then
        echo "$line_content already exists"
    else
        echo "Adding $line_content to your $ETC_HOSTS"
        printf "%s\t%s\n" "$IP" "$HOSTNAME" >> "$ETC_HOSTS"

        if grep -qE "$HOSTNAME" "$ETC_HOSTS"; then
            echo "$line_content was added successfully"
        else
            log_error "Failed to add $line_content, Try again!"
            return 1
        fi

        if [[ "$IP" == "127.0.0.1" ]]; then
            printf "%s\t\t%s\n" "::1" "$HOSTNAME" >> "$ETC_HOSTS"
        fi
    fi
}

### Dispatch ###

case "${1:-}" in
    add)    add "$2" "$3" ;;
    remove) remove "$2" "$3" ;;
    *)
        log_error "Action inconnue: ${1:-<vide>}"
        echo "Usage: ./manage_hosts.sh {add|remove} <IP> <HOSTNAME>"
        exit 1
        ;;
esac
