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

# Same argument order as internal/steam/install.go: force_install_dir must come before login, otherwise steamcmd
# says "Please use force_install_dir before logon!". The windows depot also contains the linux acServer binary.
# No "validate": it is pointless for a fresh install. steamcmd's exit status is captured on purpose (set -e must not
# swallow the diagnostics below).
#
# STEAM_STRACE=1 runs steamcmd under strace (installed on demand; the CA step above makes apt work behind a proxy).
# Only file-related syscalls are traced (no read/write), so the typed password cannot end up in the trace.
if [ "${STEAM_STRACE:-}" = 1 ]; then
    if ! command -v strace >/dev/null 2>&1; then
        apt-get update
        apt-get install -y --no-install-recommends strace
    fi
    set -- strace -f -s 200 -e trace=file -o /tmp/steam.strace steamcmd.sh
else
    set -- steamcmd.sh
fi

status=0
"$@" \
    +@sSteamCmdForcePlatformType windows \
    +force_install_dir /base \
    +login "$STEAM_USER" \
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
    if [ "${STEAM_STRACE:-}" = 1 ] && [ -f /tmp/steam.strace ]; then
        # Failed file creations (ENOENT = a missing directory): the last 25 plus the 5 trace lines before the last one.
        # Only open/mkdir/rename style syscalls are in the trace, no read/write, so no credentials are printed.
        grep -E "^[0-9]+ +(openat?|creat|mkdirat?|rename(at2?)?)\(" /tmp/steam.strace \
            | grep -E "(O_CREAT|O_WRONLY|O_RDWR|mkdir|rename|creat\()" | grep -E "= -1 ENOENT" > /tmp/steam.enoent || true
        echo "----- failed creating syscalls (ENOENT), last 25 -----" >&2
        tail -n 25 /tmp/steam.enoent >&2
        last=$(tail -n 1 /tmp/steam.enoent)
        if [ -n "$last" ]; then
            echo "----- 5 trace lines before the last one -----" >&2
            grep -n -F -x -e "$last" /tmp/steam.strace | tail -n 1 | cut -d: -f1 | {
                read -r n || exit 0
                [ "$n" -gt 1 ] && sed -n "$((n > 5 ? n - 5 : 1)),$((n - 1))p" /tmp/steam.strace >&2
            } || true
        fi
        for d in /root/Steam /opt/steamcmd /base; do
            [ -d "$d" ] || continue
            echo "----- ls -laR $d (depth 3) -----" >&2
            find "$d" -maxdepth 3 -not -path "/opt/steamcmd/package/*" -not -path "/opt/steamcmd/siteserverui/*" -not -path "/opt/steamcmd/public/*" -not -path "/opt/steamcmd/linux*/*" -not -path "*/steamapps/downloading/*/*" -exec ls -lad {} + 2>&1 | head -n 150 >&2 || true
        done
    fi
    exit 1
fi

chown -R assetto:assetto /base
echo "Assetto Corsa server installed into the ac-base volume."
