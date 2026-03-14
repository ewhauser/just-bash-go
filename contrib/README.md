# Contrib Commands

`contrib/` holds optional `gbash` extensions that should not inflate the core runtime module.

Rules for this directory:

- each direct child under `contrib/` is its own Go module
- contrib commands are opt-in and are not registered by `commands.DefaultRegistry()`
- contrib is where heavyweight or niche commands live so the main `github.com/ewhauser/gbash` module stays small

Today that includes:

- `contrib/awk` for the optional sandboxed `awk` command
- `contrib/extras` for a convenience helper that builds a registry with the stable contrib commands enabled
- `contrib/jq` for the optional sandboxed `jq` command and its JSON/query stack
- `contrib/nodejs` for the optional experimental sandboxed `nodejs` command backed by `goja` and a curated `goja_nodejs` allowlist; it is intentionally not included in `contrib/extras` yet, and its module-level design notes live in `contrib/nodejs/README.md`
- `contrib/sqlite3` for the optional sandboxed `sqlite3` command
- `contrib/yq` for the optional sandboxed `yq` command and its YAML/query stack
