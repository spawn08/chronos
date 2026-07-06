# Chronos Documentation Website

The Chronos documentation site, built with [Docusaurus](https://docusaurus.io/).
Content is ported from the original Jekyll site under `../docs/` and served at
the site root so URLs match the original permalinks (e.g.
`/getting-started/quickstart`).

## Install

```bash
npm install
```

## Local Development

```bash
npm start
```

Starts a local dev server (default http://localhost:3000/chronos/) with live reload.

## Build

```bash
npm run build   # static output in ./build
npm run serve   # preview the production build locally
npm run typecheck
```

## Structure

- `docs/` — documentation content (getting-started, guides, api, reference, deployment)
- `sidebars.ts` — manual sidebar mirroring the original navigation
- `src/pages/index.tsx` — homepage
- `docusaurus.config.ts` — site config (`routeBasePath: '/'`, blog disabled)

## Deployment

Configured for GitHub Pages at `https://spawn08.github.io/chronos/`:

```bash
GIT_USER=<GitHub username> npm run deploy
```
