// Package server exposes a shared Unix-socket server mode for gbash runtimes.
//
// The package is intentionally small and host-oriented. It lets embedders and
// shipped CLIs serve persistent gbash sessions over a Unix-socket JSON-RPC
// endpoint without committing to a public client SDK.
package server
