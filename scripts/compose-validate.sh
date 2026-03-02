#!/usr/bin/env bash
set -euo pipefail

LOCAL_ENV_FILE=.env.local.example docker compose --env-file .env.local.example -f docker-compose.yml -f docker-compose.local.yml config >/dev/null
PI_ENV_FILE=.env.pi.example docker compose --env-file .env.pi.example -f docker-compose.yml -f docker-compose.pi.yml config >/dev/null

echo "docker compose configuration is valid"
