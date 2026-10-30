#!/bin/sh
# Prepares the volumes of the multiserver setup from the pristine install in /base. Safe to run repeatedly.
#   seed.sh shared    populates the volume shared by all instances (content, shared data)
#   seed.sh instance  gives one instance its own copy of the install (everything except content)
set -eu

if [ ! -d /base/system ]; then
    echo "The AC server is not installed yet. Run: docker compose --profile install run --rm install" >&2
    exit 1
fi

case "${1:-}" in
shared)
    if [ ! -e /content/.seeded ]; then
        cp -a /base/content/. /content/
        touch /content/.seeded
    fi

    mkdir -p /shared
    chown -R assetto:assetto /content /shared
    ;;
instance)
    # content is mounted from the shared volume at runtime
    if [ ! -d /instance/system ]; then
        tar -C /base --exclude=./content -cf - . | tar -C /instance -xf -
    fi

    chown -R assetto:assetto /instance
    ;;
*)
    echo "usage: seed.sh shared|instance" >&2
    exit 2
    ;;
esac
