#!/bin/sh
set -e

if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable pairflix-webserver.service
    # Start on install, restart on upgrade. A failed start (e.g. no
    # Chromecast reachable yet) must not fail the package install.
    systemctl restart pairflix-webserver.service || true
fi
