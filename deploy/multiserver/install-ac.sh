#!/bin/sh
# Installs the Assetto Corsa dedicated server (steam app 302550) into /base with steamcmd.
# Run interactively: steamcmd asks for the password and the Steam Guard code on the terminal.
set -eu

# Behind a TLS-intercepting proxy steamcmd needs the proxy's root CA (compose secret ca_cert = $EXTRA_CA_CERT).
# The default secret is empty. INSTALL_AC_CA_ONLY=1 stops right after this step (used to test the network setup).
if [ -s /run/secrets/ca_cert ]; then
    cp /run/secrets/ca_cert /usr/local/share/ca-certificates/extra-ca.crt
    update-ca-certificates
fi
if [ "${INSTALL_AC_CA_ONLY:-}" = 1 ]; then
    exit 0
fi

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
