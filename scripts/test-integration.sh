#!/bin/sh
set -eu
# Ignore the application's COMPOSE_PROJECT_NAME and isolate concurrent proofs.
test_project="destekmasa-tests-$(date +%s)-$$"
cleanup() {
    docker compose -p "$test_project" -f compose.test.yaml down --volumes
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
docker compose -p "$test_project" -f compose.test.yaml up --build --abort-on-container-exit --exit-code-from integration
