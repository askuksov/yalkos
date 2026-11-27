# Installing Yalkos

Yalkos runs as two containers: the app (`ghcr.io/askuksov/yalkos`) and PostgreSQL. A compose file ships with every release. This page covers installing it, putting it behind a domain with HTTPS, upgrades, backup and restore, configuration and removal.

- [Requirements](#requirements)
- [Install](#install)
- [First run](#first-run)
- [Domain and HTTPS](#domain-and-https)
- [Upgrade](#upgrade)
- [Backup and restore](#backup-and-restore)
- [Configuration](#configuration)
- [Operations](#operations)
- [Uninstall](#uninstall)
- [Without Docker](#without-docker)

## Requirements

- A Linux server, amd64 or arm64, with 1 GB of RAM.
- Docker Engine 25 or newer with the Compose plugin. The compose file uses a health check start interval that older engines reject.
- `curl` and `openssl` (both are on standard Ubuntu server images).
- For HTTPS: a domain name whose DNS record points at the server, and ports 80 and 443 open. A trial without a domain works on plain HTTP.

Docker's convenience script installs the newest engine and the Compose plugin on Ubuntu, Debian and other common distributions:

```sh
sudo apt-get update && sudo apt-get install -y curl openssl ca-certificates
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"
```

Log out and back in so that your user can run `docker` without `sudo`, then check:

```sh
docker version --format '{{.Server.Version}}'
docker compose version
```

For other systems, or to install from Docker's package repository instead of the script, follow [Docker's installation guide](https://docs.docker.com/engine/install/).

If Docker came from your distribution's own packages (Ubuntu's `docker.io`, for example) and `docker compose version` reports an unknown command, install the Compose plugin from the same repository:

```sh
sudo apt-get install -y docker-compose-v2
```

## Install

Run this in a new directory (the first line creates one):

```sh
mkdir yalkos && cd yalkos &&
curl -fsSLO https://github.com/askuksov/yalkos/releases/latest/download/docker-compose.yml &&
echo "YALKOS_DB_PASSWORD=$(openssl rand -hex 24)" > .env &&
docker compose up -d --wait
docker compose logs app | grep "setup required"
```

The first four lines are one chain: if one step fails, for example `mkdir` because the directory already exists (Yalkos may be installed there), the rest does not run and `.env` is not touched.

Line by line:

1. Creates a directory for the compose file, the settings and, later, the backups.
2. Downloads the compose file of the newest stable release. It pulls the app image `ghcr.io/askuksov/yalkos` and `postgres:18-alpine`; no repository checkout is needed.
3. Writes `.env` with a random database password. Run this line once: it replaces the file, and a new password no longer matches the one PostgreSQL stored when its volume was created.
4. Pulls the images and starts both containers. `--wait` returns when `app` and `db` are healthy; the first start takes a minute or so while images download and the database is initialized. Migrations run automatically.
5. Prints the one-time setup link ([First run](#first-run)).

The compose file fixes the project name to `yalkos`, so containers (`yalkos-app-1`, `yalkos-db-1`) and volumes (`yalkos_yalkos-data`, `yalkos_yalkos-pgdata`) are the same whichever directory you run in: one install per Docker host. A second install in another directory would take over the first one's containers and volumes. To run a second instance on the same host, put `COMPOSE_PROJECT_NAME=<name>` (for example `yalkos-test`) into its `.env` before its first `docker compose up`, and give it its own `YALKOS_PORT`.

Keep `.env` private, because it holds the database password:

```sh
chmod 600 .env
```

`YALKOS_DB_PASSWORD` is the password of the `yalkos` PostgreSQL user. PostgreSQL stores it when the data volume is first created, so changing the value in `.env` later is not enough. Change it in the database first, then in `.env`, then recreate the app:

```sh
docker compose exec -T db psql -U yalkos -d yalkos -c "ALTER USER yalkos PASSWORD 'new-password'"
# put the same value into YALKOS_DB_PASSWORD in .env, then:
docker compose up -d
```

Use letters and digits only: the password becomes part of a database URL.

To change settings later, edit `.env` with a text editor, or append a line with `>>` (never `>`, which replaces the whole file), then run `docker compose up -d`; compose recreates the containers whose settings changed. All settings are listed under [Configuration](#configuration).

The app listens on port 8080 of the server (`YALKOS_PORT` changes the published port). Without a domain, open `http://localhost:8080` on the server itself, or from your computer through an SSH tunnel, `ssh -L 8080:localhost:8080 user@server`, then `http://localhost:8080` locally. To reach a trial server directly by its address, set `YALKOS_BASE_URL` to exactly that address before the first start, for example `YALKOS_BASE_URL=http://203.0.113.10:8080` (plain HTTP sends passwords unencrypted; use [HTTPS](#domain-and-https) for anything real).

### A specific version

The compose file defaults to the release line of the file you downloaded (for example `0.1`, which follows patch releases). To pin an exact release, download that release's file and set the version:

```sh
mkdir yalkos && cd yalkos &&
curl -fsSLO https://github.com/askuksov/yalkos/releases/download/vX.Y.Z/docker-compose.yml &&
echo "YALKOS_DB_PASSWORD=$(openssl rand -hex 24)" > .env &&
echo "YALKOS_VERSION=X.Y.Z" >> .env &&
docker compose up -d --wait
```

Replace `X.Y.Z` with the version, for example `0.1.0` (and `vX.Y.Z` with `v0.1.0`). Image tags exist for the exact version (`0.1.0`), the release line (`0.1`) and `latest`. Releases are listed on the [releases page](https://github.com/askuksov/yalkos/releases).

## First run

A fresh instance prints a one-time setup link in its log. Whoever opens it first becomes the Owner:

```sh
docker compose logs app | grep "setup required"
# app-1  | {"time":"...","level":"WARN","msg":"setup required","url":"http://localhost:8080/setup?code=7EV2RUJH"}
```

Open the address from the `url` field. The code changes on every restart until setup is done, so use the newest line if you restarted. The link is built from `YALKOS_BASE_URL`: with the default, its host is `localhost`.

After setup, invite people from **Members**. Invite links are single-use and expire after 7 days. There is no email in this version: you send the link yourself.

## Domain and HTTPS

The steps below put [Caddy](https://caddyserver.com/) on the server in front of Yalkos. Caddy obtains and renews certificates by itself. You need a domain whose A (and AAAA) record points at the server, and ports 80 and 443 reachable from the internet. The examples use `yalkos.example.com`; replace it everywhere.

### 1. Install Caddy

From Caddy's official package repository ([Caddy's instructions](https://caddyserver.com/docs/install#debian-ubuntu-raspbian)):

```sh
sudo apt install --yes debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg
sudo chmod o+r /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install --yes caddy
```

### 2. Point Caddy at Yalkos

The whole configuration is three lines (if you changed `YALKOS_PORT`, use that port):

```sh
sudo tee /etc/caddy/Caddyfile >/dev/null <<'EOF'
yalkos.example.com {
	reverse_proxy 127.0.0.1:8080
}
EOF
sudo systemctl reload caddy
```

For a name that public certificate authorities cannot reach (a test or LAN name), use the variant in [Names public CAs cannot reach](#names-public-cas-cannot-reach) instead.

### 3. Tell Yalkos about the domain

Append three settings to `.env` (if one of these keys is already in the file, edit that line instead):

```sh
cat >> .env <<'EOF'
YALKOS_BIND=127.0.0.1
YALKOS_BASE_URL=https://yalkos.example.com
YALKOS_TRUSTED_PROXIES=172.16.0.0/12,192.168.0.0/16
EOF
docker compose up -d --wait
```

- `YALKOS_BIND=127.0.0.1` publishes the app port on the server's loopback address only, so the only way in from outside is through Caddy.
- `YALKOS_BASE_URL` must be the address people type, scheme and host without a path. Invite and setup links are built from it, and the session cookie is marked `Secure` when it starts with `https`. A state-changing request from any other origin (for example the server's IP address) is rejected with `request origin does not match BASE_URL`.
- `YALKOS_TRUSTED_PROXIES` makes the app read the client address from the `X-Forwarded-For` header sent by Caddy. The client address is used by the sign-in and setup rate limits only (10 failed sign-ins per email and client network per 15 minutes, plus a per-email limit per hour). Without it every client looks like the proxy, so ten wrong passwords from anyone lock that user's sign-in for 15 minutes.

Why this value: Caddy on the host reaches the published port through Docker's network gateway, an address from Docker's default address pools, which are the `172.16.0.0/12` and `192.168.0.0/16` ranges. If you changed `default-address-pools` in `/etc/docker/daemon.json`, list your ranges instead. Exactly one proxy hop is supported: the app trusts the rightmost `X-Forwarded-For` entry, the address your proxy saw. If a CDN or another proxy sits in front of Caddy, the proxy next to Yalkos must itself pass on the real client address.

Trust these ranges only together with `YALKOS_BIND=127.0.0.1`. Otherwise a client inside such a network (for example on your home or office LAN) could connect to the app port directly, send its own `X-Forwarded-For` header and pick the address the limiters see.

Check it:

```sh
curl -fsS https://yalkos.example.com/readyz
```

Then open `https://yalkos.example.com` and sign in. A certificate is requested on the first visit to the domain; if it fails, check DNS and ports 80 and 443 and read `sudo journalctl -u caddy`.

### Names public CAs cannot reach

For a name that public certificate authorities cannot validate (a test or LAN name), let Caddy issue the certificate from its own local authority by adding `tls internal` inside the site block:

```caddyfile
yalkos.example.com {
	tls internal
	reverse_proxy 127.0.0.1:8080
}
```

Reload Caddy after editing, as in step 2. Browsers do not trust that authority at first. On the server, `sudo caddy trust` adds it to the system trust store. On other machines, import Caddy's root certificate; for the packaged service it is `/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt` (copy it to the other machine and add it to that machine's trusted roots).

### Firewalls

Ports published by Docker bypass `ufw` and `firewalld` rules: a rule that denies port 8080 does not stop a container published on `0.0.0.0:8080`. That is why this guide binds the app to `127.0.0.1`. Keep that setting when a proxy runs on the same host, and open only 22, 80 and 443 on the firewall.

### nginx

If nginx already runs on the server, keep it and point it at `127.0.0.1:8080` with the settings from the Caddy section (`YALKOS_BIND`, `YALKOS_BASE_URL`, `YALKOS_TRUSTED_PROXIES`). Two lines matter:

```nginx
server {
    listen 443 ssl;
    server_name yalkos.example.com;
    # ssl_certificate and ssl_certificate_key, for example from certbot

    client_max_body_size 30m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

- `client_max_body_size` must be at least `MAX_UPLOAD_MB` (25 by default) plus a few megabytes. nginx's default is 1 MB, which rejects larger uploads with `413`.
- `proxy_set_header X-Forwarded-For $remote_addr` replaces whatever the client sent with the address nginx saw.

The `/mcp` endpoint is plain request and response over POST, so no buffering or timeout tuning is needed.

## Upgrade

Releases never remove your data, but migrations only move forward, so take a [backup](#backup-and-restore) first. Then, in the directory with `docker-compose.yml` and `.env`:

```sh
docker compose pull
docker compose up -d --wait
docker compose exec app /yalkos version
```

`pull` fetches the newest image for the tag in use (and any newer `postgres:18-alpine` patch); `up -d` recreates the changed containers; the app applies pending migrations before it starts listening. Read the release notes on the [releases page](https://github.com/askuksov/yalkos/releases) first.

With the default settings the tag is the release line (`0.1`), so `pull` brings patch releases but never a new minor version. For a new minor (for example `0.2`), download that release's compose file over the old one, which also moves the default; `.env` stays as it is. If `.env` pins `YALKOS_VERSION`, change it there instead:

```sh
curl -fsSLO https://github.com/askuksov/yalkos/releases/latest/download/docker-compose.yml
docker compose pull
docker compose up -d --wait
```

PostgreSQL major versions (the `postgres:18-alpine` image) are not changed by upgrades of Yalkos; moving the database to a new PostgreSQL major version is a separate dump-and-restore procedure that this page does not cover.

### Rollback

An older image does not run on a database that a newer version has migrated: `/readyz` stays at `503` and the app reports pending migrations. To go back, restore the backup you took before the upgrade, running the old version:

1. Set `YALKOS_VERSION=<old version>` in `.env` (for example `0.1.0`).
2. Follow [Restore](#restore) with the backup taken before the upgrade. Its last step starts the app with the pinned version.

## Backup and restore

A complete backup is two files: the database dump and an archive of the uploaded files (the `/data` volume). Take both together. Run in the directory with `docker-compose.yml` and `.env`:

```sh
docker compose exec -T db pg_dump -U yalkos yalkos > yalkos.sql.tmp &&
docker run --rm --volumes-from "$(docker compose ps -aq app)" -v "$PWD":/backup \
  -e OWNER="$(id -u):$(id -g)" alpine \
  sh -c 'tar czf /backup/yalkos-data.tgz.tmp -C /data . && chown "$OWNER" /backup/yalkos-data.tgz.tmp' &&
mv yalkos.sql.tmp yalkos.sql &&
mv yalkos-data.tgz.tmp yalkos-data.tgz &&
chmod 600 yalkos.sql yalkos-data.tgz
```

The first command writes the dump, the second archives the files into `yalkos-data.tgz.tmp`, found through the app container rather than a volume name, so it is independent of the compose project name. Only when both succeeded are they renamed to `yalkos.sql` and `yalkos-data.tgz`, so a failed step (a stopped database, a full disk) stops the chain and leaves no file that looks like a backup; a leftover `.tmp` file is incomplete and can be deleted. Both files hold everything in the workspace, password hashes included: keep them private and copy them off the server, since a backup on the same disk does not survive the disk. A new backup replaces files of the same name; rename or move the old ones first if you want to keep them. For a copy where database and files are guaranteed to match, run `docker compose stop app` before and `docker compose start app` after.

### Restore

Restoring starts from a directory with `docker-compose.yml` and `.env`, both data volumes empty or about to be replaced, and the two backup files `yalkos.sql` and `yalkos-data.tgz` in the same directory. On a new server, create the directory with `mkdir yalkos && cd yalkos` and copy those four files into it; the `.env` of the old server is the easiest choice, but a new `.env` with a new password works too, because the dump holds no database passwords. Use the compose file of the Yalkos version that made the backup, or a newer one.

The app must not run until both parts are back: a started app would initialize an empty schema, and stored files that no database row references are removed after 24 hours.

```sh
docker compose stop app &&
docker compose up -d --wait db &&
docker compose create app &&
docker compose exec -T db dropdb -U yalkos --if-exists --force yalkos &&
docker compose exec -T db createdb -U yalkos -O yalkos yalkos &&
docker compose exec -T db psql -U yalkos -d yalkos -v ON_ERROR_STOP=1 -q -o /dev/null < yalkos.sql &&
docker run --rm --volumes-from "$(docker compose ps -aq app)" -v "$PWD":/backup alpine \
  sh -c 'find /data -mindepth 1 -delete && tar xzf /backup/yalkos-data.tgz -C /data && chown -R 65532:65532 /data' &&
docker compose up -d --wait
```

What the commands do: stop the app if it runs; start only the database; create the app container without starting it (the file archive is unpacked through its volume); recreate the empty `yalkos` database and load the dump; replace the contents of `/data` and give them to the user the app runs as (uid 65532, which matters because the data directory is private to it); start everything. The commands are one chain, and `psql` stops at the first error: if the dump does not load, the files in `/data` stay untouched and the app is not started. Fix the cause and run the whole block again; it recreates the database.

The dump must come from the same or an older Yalkos version than the image you restore into, because the app migrates forward on start and cannot migrate back.

Sign in with your old account to check the result. Run one app container per data volume.

## Configuration

Settings go into `.env`, next to the compose file. The compose file passes them on to the app; an empty or missing optional value means "use the app's default".

### Compose variables

| Variable | Default | Meaning |
| :--- | :--- | :--- |
| `YALKOS_DB_PASSWORD` | none (required) | Password of the `yalkos` PostgreSQL user; see [Install](#install) before changing it later |
| `COMPOSE_PROJECT_NAME` | `yalkos` (the `name:` of the compose file) | Compose project name; it prefixes container and volume names. Set it only for a second instance on the same host, before its first start |
| `YALKOS_VERSION` | the release line of the downloaded file, for example `0.1` | Image tag: `0.1` (follows patch releases), `0.1.0` (exact) or `latest` |
| `YALKOS_PORT` | `8080` | Port published on the server |
| `YALKOS_BIND` | empty (all interfaces) | Address the port is published on; `127.0.0.1` when a proxy runs on the same host |
| `YALKOS_BASE_URL` | `http://localhost:` plus `YALKOS_PORT` | Passed as `BASE_URL` |
| `YALKOS_LOG_LEVEL` | empty (app default) | Passed as `LOG_LEVEL` |
| `YALKOS_MAX_UPLOAD_MB` | empty (app default) | Passed as `MAX_UPLOAD_MB` |
| `YALKOS_API_RATE_LIMIT` | empty (app default) | Passed as `API_RATE_LIMIT` |
| `YALKOS_WEBHOOK_ALLOW_PRIVATE` | empty (app default) | Passed as `WEBHOOK_ALLOW_PRIVATE` |
| `YALKOS_TRUSTED_PROXIES` | empty (app default) | Passed as `TRUSTED_PROXIES` |

### App variables

The app reads these from its environment. In the compose setup `DATABASE_URL` and `DATA_DIR` are set by the compose file and `LISTEN_ADDR` is left at its default; change the published port with `YALKOS_PORT`.

| Variable | Default | Meaning |
| :--- | :--- | :--- |
| `DATABASE_URL` | none (required) | PostgreSQL connection string; PostgreSQL 18 or newer |
| `LISTEN_ADDR` | `:8080` | HTTP listen address inside the container (`yalkos healthcheck` reads it too) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `BASE_URL` | `http://localhost:8080` | Public address, scheme and host without a path. Used for invite and setup links and to decide whether cookies are `Secure` (`https`); state-changing requests from another origin are rejected |
| `DATA_DIR` | `./data` (`/data` in the image) | Uploaded files, created with mode 0700 |
| `MAX_UPLOAD_MB` | `25` | Largest file to upload, 1 to 1024 |
| `API_RATE_LIMIT` | `5000` | API key requests per hour and user on every endpoint, 0 to 1000000; `0` turns the limit off |
| `WEBHOOK_ALLOW_PRIVATE` | `true` | `false` blocks webhook destinations on loopback and private networks; link-local and cloud metadata addresses are always blocked |
| `TRUSTED_PROXIES` | empty | Comma-separated IP addresses and CIDR ranges (IPv4 or IPv6) of the reverse proxy; the app reads `X-Forwarded-For` only from these peers and takes its rightmost entry as the client address. Empty ignores the header. An invalid item stops the start with `TRUSTED_PROXIES: "<item>" is not an IP address or CIDR range`. See [Domain and HTTPS](#domain-and-https) |

An invalid value stops the app at start with a message naming the variable; read it with `docker compose logs app`.

## Operations

- **Logs** are JSON lines on stdout: `docker compose logs -f app` (use `db` for PostgreSQL). Request logs hold no client addresses. Set `YALKOS_LOG_LEVEL=debug` for more detail.
- **Health**: `GET /healthz` answers `200` once the server listens; `GET /readyz` answers `200` when the database answers and its migrations match the app, otherwise `503`. `docker compose ps` shows `healthy` once the container's own check (`yalkos healthcheck`, which asks `/readyz`) passes. To run the check by hand:

  ```sh
  docker compose exec app /yalkos healthcheck
  ```

  It prints `ready` and exits 0 when the app is ready.
- **Version**: `docker compose exec app /yalkos version`.
- **Restarts**: containers use `restart: unless-stopped`, so they come back after a reboot as long as the Docker service starts at boot (the default after the install script above).
- **Password reset**. There is no email in this version. Reset a password from the server:

  ```sh
  docker compose exec -T app /yalkos user set-password --email you@example.com <<< 'new-password'
  ```

- **Import from Plane**: `yalkos import plane` is described in [the README](../README.md#import-from-plane).

## Uninstall

In the directory with `docker-compose.yml` and `.env`:

```sh
docker compose down
```

removes the containers and the network and keeps your data (the two volumes), so `docker compose up -d` brings everything back.

```sh
docker compose down -v
```

also deletes the database and the uploaded files. This cannot be undone; take a [backup](#backup-and-restore) first if you may need the data.

```sh
docker compose down -v --rmi all
```

additionally removes the images the compose file uses. Last, delete the directory itself with `cd .. && rm -rf yalkos`; this also deletes `.env` (the database password) and any backups saved there. If you installed Caddy only for Yalkos, remove it with `sudo apt-get purge -y caddy`. The `alpine` image that [Backup and restore](#backup-and-restore) pulls can be removed with `docker image rm alpine`.

## Without Docker

Yalkos is a single binary with the web UI embedded: `make build` produces `bin/yalkos` (see the README's Development section), and `bin/yalkos serve` runs it with the variables from [Configuration](#configuration) in its environment. It needs PostgreSQL 18 or newer; this page does not cover running it that way further, and the Docker install is the supported one.
