#!/bin/sh
set -e

if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
fi

if [ "$1" = "purge" ]; then
    rm -rf /var/lib/pairflix-webserver
fi
