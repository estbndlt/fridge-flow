#!/usr/bin/env bash
set -euo pipefail

PI_ENV_FILE=${PI_ENV_FILE:-$HOME/.config/fridge-flow/env.pi}
LOCAL_BUILD=${LOCAL_BUILD:-no}
LOCAL_APP_IMAGE=${LOCAL_APP_IMAGE:-fridge-flow-local}
LOCAL_APP_TAG=${LOCAL_APP_TAG:-latest}

if [[ ! -f "${PI_ENV_FILE}" ]]; then
  echo "Missing Pi env file: ${PI_ENV_FILE}" >&2
  echo "Create it with ./scripts/pi-init-env.sh or set PI_ENV_FILE to an existing file." >&2
  exit 1
fi

export PI_ENV_FILE

if [[ "${LOCAL_BUILD}" == "yes" ]]; then
  docker build -t "${LOCAL_APP_IMAGE}:${LOCAL_APP_TAG}" .
  APP_IMAGE="${LOCAL_APP_IMAGE}" APP_TAG="${LOCAL_APP_TAG}" docker compose \
    --env-file "${PI_ENV_FILE}" \
    -f docker-compose.yml \
    -f docker-compose.pi.yml \
    up -d --remove-orphans --pull never
  exit 0
fi

docker compose \
  --env-file "${PI_ENV_FILE}" \
  -f docker-compose.yml \
  -f docker-compose.pi.yml \
  pull

docker compose \
  --env-file "${PI_ENV_FILE}" \
  -f docker-compose.yml \
  -f docker-compose.pi.yml \
  up -d --remove-orphans
