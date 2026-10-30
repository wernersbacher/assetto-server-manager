#!/bin/sh
# Installs the Assetto Corsa dedicated server (steam app 302550) into /base with steamcmd.
# Run interactively: steamcmd asks for the password and the Steam Guard code on the terminal.
set -eu

if [ -z "${STEAM_USER:-}" ]; then
    printf 'Steam username: '
    read -r STEAM_USER
fi

# force_install_dir has to come before the login. The windows depot also contains the linux acServer binary.
steamcmd.sh \
    +@sSteamCmdForcePlatformType windows \
    +force_install_dir /base \
    +login "$STEAM_USER" \
    +app_update 302550 validate \
    +quit

if [ ! -d /base/system ]; then
    echo "Installation failed: /base/system does not exist" >&2
    exit 1
fi

chown -R assetto:assetto /base
echo "Assetto Corsa server installed into the ac-base volume."
