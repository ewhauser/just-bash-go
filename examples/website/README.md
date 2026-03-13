# `examples/website`

This is a vendored copy of Vercel's `just-bash` website example, adapted to run
`gbash` in the browser via `js/wasm`.

The important difference is the shell boundary:

- upstream browser shell: `just-bash/browser`
- this browser shell: `gbash` compiled to WebAssembly

## What's in this app

- `app/`
  Vendored website UI, terminal, routes, and styles
- `browser/gbash-browser.ts`
  The TypeScript compatibility layer that exports `Bash` and `defineCommand`
- `wasm/main.go`
  The Go-to-browser bridge that exposes a persistent `gbash` session
- `scripts/build-wasm.sh`
  Builds `public/gbash.wasm` and copies Go's matching `wasm_exec.js`
- `scripts/fetch-agent-data.mjs`
  Copies local `gbash` source into `app/api/agent/_agent-data` for the optional
  server-side agent route

## Local development

```bash
cd examples/website
pnpm install
pnpm dev
```

`pnpm dev` does three things before starting Next:

1. builds `gbash.wasm` from local Go source
2. copies local repo files into `app/api/agent/_agent-data`
3. starts the Next dev server

## Source-control deployment

This app is intended to be deployable directly from this repository.

For a Vercel project:

- Root Directory: `examples/website`
- Install Command: `pnpm install`
- Build Command: `pnpm build`

`pnpm build` builds the WASM artifact from source and prepares the local
agent-data snapshot before running `next build`.

## Optional agent route

The browser shell does not need the agent backend.

If you want the `agent` command to work, the server runtime also needs:

- `ANTHROPIC_API_KEY`
- optionally `AI_MODEL`

Without those, `/api/agent` returns a `503` with a setup message instead of
crashing.

## Notes

- `public/gbash.wasm` and `public/wasm_exec.js` are generated, not committed.
- Host-backed filesystems remain unsupported on `js/wasm`; the browser shell
  uses the normal in-memory `gbash` filesystem.
- This app vendors the Vercel website code so deployment does not depend on
  cloning external repositories at build time.

