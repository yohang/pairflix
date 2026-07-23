#!/bin/sh
set -e

if [ "$1" = "remove" ] && [ -d /run/systemd/system ]; then
    systemctl stop pairflix-webserver.service || true
    systemctl disable pairflix-webserver.service || true
fi
