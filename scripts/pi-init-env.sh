#!/usr/bin/env bash
set -euo pipefail

PI_ENV_FILE=${PI_ENV_FILE:-$HOME/.config/fridge-flow/env.pi}
PI_ENV_DIR=$(dirname "${PI_ENV_FILE}")

if [[ -e "${PI_ENV_FILE}" ]]; then
  echo "Refusing to overwrite existing env file: ${PI_ENV_FILE}" >&2
  exit 1
fi

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate the database password." >&2
  exit 1
fi

mkdir -p "${PI_ENV_DIR}"
chmod 700 "${PI_ENV_DIR}"

db_password=$(openssl rand -hex 24)
sed "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=${db_password}/" .env.pi.example >"${PI_ENV_FILE}"
chmod 600 "${PI_ENV_FILE}"

cat <<EOF
Created ${PI_ENV_FILE}

Next steps:
1. Fill in GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and TUNNEL_TOKEN.
2. Confirm APP_IMAGE, APP_TAG, and APP_BASE_URL.
3. Deploy with ./scripts/pi-deploy.sh
EOF
