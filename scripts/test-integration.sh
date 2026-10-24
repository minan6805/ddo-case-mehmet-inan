#!/bin/sh
set -eu
cleanup() {
    docker compose -f compose.test.yaml down --volumes
}
trap cleanup EXIT
docker compose -f compose.test.yaml up --build --abort-on-container-exit --exit-code-from integration
