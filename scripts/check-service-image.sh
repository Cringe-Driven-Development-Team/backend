#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 2 ]]; then
    echo "Usage: $0 <identity|workspace|environments|onlineconf> <image>" >&2
    exit 2
fi

service=$1
image=$2
case "$service" in
    identity) startup_error='identity.startup' ;;
    workspace) startup_error='workspace.failed' ;;
    environments) startup_error='environments.failed' ;;
    onlineconf) startup_error='onlineconf.run' ;;
    *) echo "Unknown service: $service" >&2; exit 2 ;;
esac

expect_startup_error() {
    local expected=$1 output status=0
    shift
    output=$(docker run --rm --network none "$image" "$@" 2>&1) || status=$?
    if [[ $status != 1 || $output != *"$expected"* ]]; then
        printf 'Expected %s to exit with status 1 and report %s (status %s)\n%s\n' \
            "$image ${*:-default command}" "$expected" "$status" "$output" >&2
        return 1
    fi
}

# Без сервисных настроек команды завершаются до подключения к внешним системам.
expect_startup_error "$startup_error"
expect_startup_error 'DATABASE_URL is required' ./migrate --service "$service" status
if [[ $service == identity ]]; then
    expect_startup_error 'usage: identity-admin' ./identity-admin
fi

docker run --rm --network none -i --entrypoint /bin/sh "$image" -eu -s -- "$service" <<'SH'
test "$(id -u)" != 0
test -x "/app/$1"
for binary in identity workspace environments onlineconf server runtime-smoke; do
    if [ "$binary" != "$1" ]; then
        test ! -e "/app/$binary"
    fi
done
if [ "$1" != identity ]; then
    test ! -e /app/identity-admin
fi

if [ "$1" = environments ]; then
    ansible-playbook --version >/dev/null
    ssh -V >/dev/null 2>&1
    test -d /app/deploy/runtime
    test "$RUNTIME_WORK_DIR" = /var/lib/cellestial/runtime
    test -n "$RUNTIME_PYTHON_IMAGE"
    test -n "$RUNTIME_AGENT_VERSION"
    touch "$RUNTIME_WORK_DIR/.image-check"

    cd /app/agent
    test -s requirements.txt
    test -s cellestial-agent.service
    test -s cellestial-agent-grpc.service
    set -- cellestial_agent-*.whl
    test "$#" = 1
    test -s "$1"
    test -s "$1.sha256"
    checksum=$(sha256sum "$1")
    test "${checksum%% *}" = "$(cat "$1.sha256")"
else
    for tool in ansible-playbook python3 ssh; do
        if command -v "$tool" >/dev/null; then
            echo "Unexpected tool in $1: $tool" >&2
            exit 1
        fi
    done
    test ! -e /app/agent
fi
SH

printf 'Image check passed for %s (%s)\n' "$service" "$image"
