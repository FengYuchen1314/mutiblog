# Mutiblog

Mutiblog stores canonical articles as Markdown bundles and publishes static
HTML into `generated/public/`.

## Quick start

Use the prebuilt image on a server (the session secret is generated and stored
in `config/.secrets.yaml` automatically on the first start):

```sh
docker compose up -d
```

Do not build the image on a 1 GB server. For local development on a machine
with at least 4 GB RAM, use `docker compose -f docker-compose.yml -f
docker-compose.dev.yml up --build`.

Open `http://localhost:8080/admin/` to create the initial administrator. The
first start creates `config/config.yaml` from the bundled template if it is
missing. Configure the production `server.baseURL` before publishing.

## Operations

```sh
docker compose exec blog /app/blog-server doctor
docker compose exec blog /app/blog-server admin list
printf '%s\n' 'a-new-long-password' | docker compose exec -T blog /app/blog-server admin reset-password admin --stdin
docker compose exec blog /app/blog-server rebuild
docker compose run --rm blog /app/blog-server --root /app verify
docker compose exec blog /app/blog-server backup create
docker compose exec blog /app/blog-server backup list
docker compose exec blog /app/blog-server config validate
docker compose exec blog /app/blog-server config get site.title
docker compose exec blog /app/blog-server config set site.title '"My Blog"'
docker compose run --rm blog /app/blog-server --root /app migrate --dry-run
docker compose run --rm blog /app/blog-server --root /app migrate
docker compose run --rm blog /app/blog-server --root /app export /app/backups/site-export.zip --scope content,data,media,config
docker compose run --rm -v "$PWD/import:/import:ro" blog /app/blog-server --root /app import /import --dry-run
docker compose run --rm -v "$PWD/import:/import:ro" blog /app/blog-server --root /app import /import --locale zh-CN --status draft
```

To restore a backup, stop the service first and run:

```sh
docker compose run --rm blog /app/blog-server --root /app backup restore backups/backup-YYYYMMDD-HHMMSS.zip --yes
```

The portable source of truth is `content/`, `data/`, `media/`, and `config/`.
`generated/` and `cache/` are derived artifacts and can be rebuilt.

`export` produces the same checksummed ZIP format as backups while allowing a
portable subset through `--scope content,data,media,config`. It refuses to
overwrite an existing target archive.

`import` accepts a single Markdown file, a directory of Markdown files, or a
ZIP archive. It recognizes YAML and Hugo TOML Front Matter. Always begin with
`--dry-run`: it reports the proposed slugs and statuses without writing. Files
without Front Matter become drafts using their filename and modification time;
duplicate slugs get a numeric suffix, and unknown category/tag display names
are created as reusable taxonomy entries.

## Recovery shortcuts

- Forgot a password: `docker compose exec blog /app/blog-server admin reset-password admin`
- Pages look stale: `docker compose run --rm blog /app/blog-server --root /app verify --fix`
- `generated/` was removed: run `docker compose exec blog /app/blog-server rebuild` (or restart the service).
- To take the complete portable blog elsewhere: `tar czf my-blog-backup.tar.gz content/ data/ media/ config/`

For HTTPS, set `BLOG_DOMAIN` and start the recommended Caddy overlay:

```sh
BLOG_DOMAIN=blog.example.com docker compose -f docker-compose.yml -f deploy/compose.caddy.yml up -d
```

It mounts the `generated/` parent directory so Caddy follows each atomically
switched `public` release. To run the bundled Nginx static edge instead, use:

```sh
docker compose -f docker-compose.yml -f deploy/compose.nginx.yml up -d
```

It listens on port 80 and serves published files directly from `generated/`.
Both proxy options keep already-published pages available when the CMS process
is stopped.
