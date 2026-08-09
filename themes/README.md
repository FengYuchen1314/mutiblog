# Mutiblog themes

A theme lives in `themes/<name>/`. It must provide `theme.yaml` and may provide
`settings.schema.json`. The manifest declares the eleven required static page
templates: `home`, `post`, `page`, `category`, `category_list`, `tag`,
`tag_list`, `archive`, `links`, `search`, and `not_found`.

Settings use the constrained schema documented in `docs/09-theme-system.md`.
Supported field types include text, number, boolean, select, colour, image,
localized text, code, and one-level arrays. Saved values are stored in
`data/themes/<name>.settings.yaml`; only values differing from defaults are
persisted. Unknown fields are ignored, allowing themes to evolve safely.

To derive a theme, copy `default`, change `name`, retain every required
template, then add fields to `settings.schema.json`. The backend exposes the
schema unchanged to the admin API, so ordinary settings need no custom backend
code. Build client and SSR assets into the theme's `dist/` directory before
activating a production theme.
