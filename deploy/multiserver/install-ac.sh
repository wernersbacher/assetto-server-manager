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

# Same argument order as internal/steam/install.go (login before force_install_dir). The windows depot also contains
# the linux acServer binary. No "validate": it is pointless for a fresh install. steamcmd's exit status is captured
# on purpose (set -e must not swallow the diagnostics below).
status=0
steamcmd.sh \
    +@sSteamCmdForcePlatformType windows \
    +login "$STEAM_USER" \
    +force_install_dir /base \
    +app_update 302550 \
    +quit || status=$?

if [ "$status" -ne 0 ] || [ ! -d /base/system ]; then
    echo "Installation failed: steamcmd exit status $status, /base/system $([ -d /base/system ] && echo exists || echo missing)" >&2
    for f in /root/Steam/logs/*.txt /root/Steam/logs/*.log; do
        [ -f "$f" ] || continue
        echo "----- tail of $f -----" >&2
        tail -n 40 "$f" >&2
    done
    if grep -qs -e 'Missing file permissions' -e 'No subscription' /root/Steam/logs/* 2>/dev/null; then
        echo "Hint: the Steam account must own Assetto Corsa, and ownership and the Steam Guard approval are required." >&2
        echo "Hint: repeated failed logins can trigger Steam's rate limit; wait about 30 minutes before retrying." >&2
    fi
    exit 1
fi

chown -R assetto:assetto /base
echo "Assetto Corsa server installed into the ac-base volume."
