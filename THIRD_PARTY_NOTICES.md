# Third-party notices

MutiBlog is an independent AGPL-3.0 project. It studies and interoperates with open-source projects while preserving their licenses and brands.

## Halo components

- Package: `@halo-dev/components`
- Copyright: Halo project contributors
- License: MIT
- Source: <https://github.com/halo-dev/halo/tree/main/ui/packages/components>

MutiBlog uses Halo's published public component package to implement a consistent console UI. Halo and its logo are trademarks/brand assets of their respective owners and are not MutiBlog branding.

## MathJax

- Package: `mathjax` 3.2.2
- Copyright: MathJax Consortium
- License: Apache-2.0
- Source: <https://github.com/mathjax/MathJax>

MutiBlog bundles MathJax's `tex-svg` browser component into the console build for Markdown-preview formulae. It is served from the same origin, uses self-contained SVG output, and has dynamic TeX module loading disabled for untrusted Markdown.

## Product references

- Halo — GPL-3.0; product and interaction reference only: <https://github.com/halo-dev/halo>
- Halo Earth theme — GPL-3.0; visual/product reference for MutiBlog's independently implemented React theme: <https://github.com/halo-dev/theme-earth>
- HedgeDoc — AGPL-3.0; Markdown editor interaction reference: <https://github.com/hedgedoc/hedgedoc>

## Go libraries

- Package: `github.com/skip2/go-qrcode`
- Copyright: Tom Harwood and go-qrcode contributors
- License: MIT
- Source: <https://github.com/skip2/go-qrcode>

MutiBlog uses go-qrcode only for local, same-origin share QR image generation; the library does not make network requests.

The dependency lockfile contains the complete machine-readable JavaScript dependency graph. Go dependencies are recorded in `go.mod` and `go.sum`.
