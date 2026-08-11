# MutiBlog

MutiBlog is being rebuilt from a clean slate.

The product direction is:

- a single-site, single-administrator, self-hosted publishing system;
- a console and CMS workflow modeled on Halo 2.25 Community Edition;
- a pure Markdown editor modeled on HedgeDoc, including pasted-image uploads;
- Markdown and YAML files as the durable source of truth;
- AI-native multilingual content with editable translations and deterministic fallback;
- static public pages, with comments as an explicitly dynamic and optional surface;
- installable React SSR themes, beginning with an Earth-compatible default experience;
- one-container deployment with a low resource footprint.

The previous implementation is archived on the `old` branch and is not a reference for this rewrite.

## Current foundation

- Go core service with atomic YAML storage, setup, Argon2id login, sessions, CSRF, health checks, and static serving;
- Vue 3 console using the MIT-licensed Halo component package;
- React SSR build CLI with a first Earth-style multilingual theme;
- product research, architecture, file contracts, API map, and acceptance matrix under `docs/`;
- local, Docker, and GitHub Actions build paths.

## Local development

Requirements: Go 1.25+, Node 24+, and pnpm 11+.

```bash
pnpm install
pnpm build
go test ./...
go run ./cmd/mutiblog
```

The service listens on `http://127.0.0.1:8080` by default. The console is available at `/console/`; persistent files are written under `./data` unless `MUTIBLOG_DATA_DIR` is set.

Read [PROJECT_SPEC.md](PROJECT_SPEC.md) before changing product behavior. The previous implementation is archived only; code or design from the `old` branch must not be reused.

## License

MutiBlog is licensed under [AGPL-3.0](LICENSE). Third-party components retain their own licenses; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
