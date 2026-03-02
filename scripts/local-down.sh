#!/usr/bin/env bash
set -euo pipefail

docker compose \
  --env-file .env.local \
  -f docker-compose.yml \
  -f docker-compose.local.yml \
  down
