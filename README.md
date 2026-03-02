# FridgeFlow

FridgeFlow is a mobile-first shared grocery web app for one household. It uses Google sign-in, lets every grocery item belong to a specific store, and gives you a store-focused shopping view when you are actually inside Costco, Trader Joe's, or anywhere else you shop.

## Features

- Google OAuth login for Gmail/Google accounts
- Automatic owner bootstrap on the first sign-in
- Invite-based household access after the first owner exists
- One shared grocery list grouped by store
- Store-specific focus view for shopping
- Purchase history with one-tap re-add
- Installable PWA shell for home-screen use
- Docker Compose for local and Raspberry Pi deployment
- GHCR multi-arch image publishing on merge to `main`

## Stack

- Go 1.24
- Server-rendered HTML templates with lightweight async refresh
- PostgreSQL 16
- Docker Compose
- Cloudflare Tunnel for public Pi ingress

## Local development

1. Copy the local env template.

```bash
cp .env.local.example .env.local
```

2. Fill in `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and confirm the redirect URI is `http://localhost:3000/auth/google/callback`.

3. Start the app.

```bash
./scripts/local-up.sh
```

4. Open [http://localhost:3000](http://localhost:3000).

5. Stop the app.

```bash
./scripts/local-down.sh
```

## Google OAuth setup

- App name: `FridgeFlow`
- Local redirect URI: `http://localhost:3000/auth/google/callback`
- Production redirect URI: `https://fridgeflow.estbndlt.com/auth/google/callback`
- Production domain: `fridgeflow.estbndlt.com`

The first successful Google login creates the owner household. After that, only invited members can join.

## Docker Compose files

- `docker-compose.yml`: shared `postgres` and `web` services
- `docker-compose.local.yml`: local build plus `127.0.0.1:3000`
- `docker-compose.pi.yml`: pulled GHCR image plus managed `cloudflared`

Validate both overlays with:

```bash
./scripts/compose-validate.sh
```

## GitHub Actions package publishing

The workflow at `.github/workflows/publish-image.yml` builds and pushes a multi-arch image to GHCR on every merge to `main`.

- Image target: `ghcr.io/estbndlt/fridge-flow`
- Platforms: `linux/amd64`, `linux/arm64`
- Tags: `main`, short SHA, and git tags

## Raspberry Pi deployment

FridgeFlow is intended to run on a Raspberry Pi and publish through Cloudflare Tunnel at `https://fridgeflow.estbndlt.com`.

1. Create the external Pi env file.

```bash
./scripts/pi-init-env.sh
```

2. Edit `$HOME/.config/fridge-flow/env.pi` and set:

- `GOOGLE_CLIENT_ID`
- `GOOGLE_CLIENT_SECRET`
- `GOOGLE_REDIRECT_URL=https://fridgeflow.estbndlt.com/auth/google/callback`
- `APP_IMAGE=ghcr.io/estbndlt/fridge-flow`
- `APP_TAG=main` or a pinned SHA tag
- `APP_BASE_URL=https://fridgeflow.estbndlt.com`
- `TUNNEL_TOKEN`

3. Deploy on the Pi.

```bash
./scripts/pi-deploy.sh
```

4. Verify health:

```bash
curl -fsS https://fridgeflow.estbndlt.com/healthz
```

## Cloudflare

- Create or reuse a Cloudflare Tunnel on the Pi.
- Add the hostname `fridgeflow.estbndlt.com`.
- Route it to `http://127.0.0.1:8080`.
- Put the managed tunnel token in `TUNNEL_TOKEN`.

If you enable a strict Cloudflare browser challenge on the app hostname, relax it if it interferes with the app CSP or Google OAuth callback flow.

## Routes

- `GET /healthz`
- `GET /login`
- `GET /auth/google/start`
- `GET /auth/google/callback`
- `POST /auth/logout`
- `GET /`
- `GET /items/fragment`
- `POST /items`
- `POST /items/{id}/update`
- `POST /items/{id}/purchase`
- `POST /items/{id}/restore`
- `POST /items/{id}/delete`
- `GET /stores/{id}/focus`
- `GET /history`
- `GET /stores`
- `POST /stores`
- `POST /stores/{id}/archive`
- `GET /settings/members`
- `POST /settings/members/invite`
- `POST /settings/members/{id}/remove`

## Tests

```bash
go test ./...
```
