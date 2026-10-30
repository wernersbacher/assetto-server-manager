#!/bin/sh
# Prepares the volumes of the multiserver setup from the pristine install in /base. Safe to run repeatedly.
#   seed.sh content  populates the content volume shared by all servers
#   seed.sh slot     gives one server its own copy of the install (everything except content)
set -eu

if [ ! -d /base/system ]; then
    echo "The AC server is not installed yet. Run: docker compose --profile install run --rm install" >&2
    exit 1
fi

case "${1:-}" in
content)
    if [ ! -e /content/.seeded ]; then
        cp -a /base/content/. /content/
        touch /content/.seeded
    fi

    chown -R assetto:assetto /content
    ;;
slot)
    # content is mounted from the shared volume at runtime
    if [ ! -d /slot/system ]; then
        tar -C /base --exclude=./content -cf - . | tar -C /slot -xf -
    fi

    chown -R assetto:assetto /slot
    ;;
*)
    echo "usage: seed.sh content|slot" >&2
    exit 2
    ;;
esac
