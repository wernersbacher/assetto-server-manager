#!/bin/sh
# Imports an Assetto Corsa dedicated server folder (e.g. downloaded on Windows) into the ac-base volume, as an
# alternative to the steamcmd "install" service. See README.md.
#
#   sh import-server.sh <path-to-server-folder> [--force]
#
# Environment:
#   AC_BASE_VOLUME  target volume (default: <compose project>_ac-base = assetto-multiserver_ac-base)
#   AC_IMAGE        image used for the copy (default: assetto-server-manager:local, built by "docker compose build")
set -eu

usage() {
    echo "usage: sh import-server.sh <path-to-server-folder> [--force]" >&2
    exit 2
}

DIR=
FORCE=0
for arg in "$@"; do
    case "$arg" in
    --force) FORCE=1 ;;
    -h | --help) usage ;;
    -*) usage ;;
    *)
        [ -z "$DIR" ] || usage
        DIR=$arg
        ;;
    esac
done
[ -n "$DIR" ] || usage

IMAGE=${AC_IMAGE:-assetto-server-manager:local}
VOLUME=${AC_BASE_VOLUME:-${COMPOSE_PROJECT_NAME:-assetto-multiserver}_ac-base}

# Git Bash on Windows would rewrite /base, /src, ... in the docker arguments
MSYS_NO_PATHCONV=1
export MSYS_NO_PATHCONV

fail() {
    echo "Error: $*" >&2
    exit 1
}

[ -d "$DIR" ] || fail "$DIR is not a directory"
for d in system content; do
    [ -d "$DIR/$d" ] || fail "$DIR/$d is missing: this is not an Assetto Corsa dedicated server folder"
done
[ -f "$DIR/acServer" ] || fail "$DIR/acServer is missing (the Linux binary; it is part of the Windows download)"
if [ "$(head -c 4 "$DIR/acServer" | od -An -c | tr -d ' ')" != '177ELF' ]; then
    fail "$DIR/acServer is not a Linux (ELF) binary"
fi

# absolute path; "pwd -W" gives the Windows path in Git Bash, which docker needs
SRC=$(cd "$DIR" && { pwd -W 2>/dev/null || pwd; })

docker image inspect "$IMAGE" >/dev/null 2>&1 ||
    fail "image $IMAGE not found. Run 'docker compose build' in deploy/multiserver first."

if [ -n "$(docker run --rm --user root -v "$VOLUME":/base --entrypoint sh "$IMAGE" -c 'ls -A /base')" ]; then
    if [ "$FORCE" -ne 1 ]; then
        fail "volume $VOLUME is not empty. Use --force to empty it first (this deletes the current install)."
    fi
    echo "Emptying volume $VOLUME ..."
    docker run --rm --user root -v "$VOLUME":/base --entrypoint sh "$IMAGE" -c 'find /base -mindepth 1 -delete'
fi

echo "Importing $SRC into volume $VOLUME ..."
docker run --rm --user root -v "$VOLUME":/base -v "$SRC":/src:ro --entrypoint sh "$IMAGE" -c \
    'cp -r /src/. /base/ && chmod +x /base/acServer && chown -R assetto:assetto /base'

echo
echo "Summary of the volume $VOLUME:"
docker run --rm --user root -v "$VOLUME":/base --entrypoint sh "$IMAGE" -c '
    echo "  size:   $(du -sh /base | cut -f1)"
    echo "  cars:   $(ls /base/content/cars 2>/dev/null | wc -l)"
    echo "  tracks: $(ls /base/content/tracks 2>/dev/null | wc -l)"
    ls -ld /base/acServer /base/system /base/cfg 2>&1 | sed "s/^/  /"'

echo
echo "Smoke check: starting acServer for 3 seconds ..."
docker run --rm --user assetto -w /base -v "$VOLUME":/base --entrypoint sh "$IMAGE" -c \
    'timeout 3 ./acServer 2>&1 | head -n 5' || true
echo
echo "Done. Next: docker compose up -d --build"
