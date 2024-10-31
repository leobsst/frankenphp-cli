#!/usr/bin/env bash

# run:
# ./manage-etc-hosts.sh add 10.20.1.2 test.com
# ./manage-etc-hosts.sh remove 10.20.1.2 test.com

# PATH TO YOUR HOSTS FILE
ETC_HOSTS=/etc/hosts
OS=$(uname)

if [[ "$OS" == "Darwin" ]]
then
    GREP_CMD="grep -p"
else
    GREP_CMD="grep -P"
fi

function remove() {
    # IP to add/remove.
    IP=${1:-127.0.0.1}
    # Hostname to add/remove.
    HOSTNAME=${2:-localhost}
    HOSTS_LINE="$IP[[:space:]]$HOSTNAME"
    HOSTS_LINE_LOCAL="::1[[:space:]]*$HOSTNAME"
    
    if [ -n "$($GREP_CMD $HOSTS_LINE $ETC_HOSTS)" ]
    then
        echo "$HOSTS_LINE Found in your $ETC_HOSTS, Removing now...";
        sudo sed -i".bak" "/$HOSTS_LINE/d" $ETC_HOSTS

        if [[ $IP == "127.0.0.1" ]]
        then
            if [ -n "$(grep -E "::1[[:space:]]+$HOSTNAME" $ETC_HOSTS)" ]
            then
            sudo sed -i".bak" "/$HOSTS_LINE_LOCAL/d" $ETC_HOSTS
            fi
        fi
    else
        echo "$HOSTS_LINE was not found in your $ETC_HOSTS";
    fi
}

function add() {
    # IP to add/remove.
    IP=${1:-127.0.0.1}
    # Hostname to add/remove.
    HOSTNAME=${2:-localhost}
    HOSTS_LINE="$IP[[:space:]]$HOSTNAME"
    line_content=$( printf "%s\t%s\n" "$IP" "$HOSTNAME" )
    line_content_local=$( printf "%s\t\t%s\n" "::1" "$HOSTNAME" )
    
    if [ -n "$($GREP_CMD $HOSTS_LINE $ETC_HOSTS)" ]
    then
        echo "$line_content already exists"
    else
        echo "Adding $line_content to your $ETC_HOSTS";
        sudo -- sh -c -e "echo '$line_content' >> /etc/hosts";

        if [ -n "$($GREP_CMD $HOSTNAME $ETC_HOSTS)" ]
        then
            echo "$line_content was added succesfully";
        else
            echo "Failed to Add $line_content, Try again!";
        fi

        if [[ $IP == "127.0.0.1" ]]
        then
            sudo -- sh -c -e "echo '$line_content_local' >> /etc/hosts";
        fi
    fi
}

$@