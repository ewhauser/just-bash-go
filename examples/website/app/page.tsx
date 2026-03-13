"use client";

import { useEffect, useState } from "react";
import TerminalComponent from "./components/Terminal";
import { TerminalData } from "./components/TerminalData";

const NOSCRIPT_CONTENT = `
  gbash (WASM)

  A local browser demo of gbash, a deterministic, sandbox-only,
  bash-like runtime for AI agents implemented in Go.

  This page vendors a terminal website UI, but the shell itself is gbash
  compiled to WebAssembly.

  QUICK START
  -----------

  go get github.com/ewhauser/gbash
  go install github.com/ewhauser/gbash/cmd/gbash@latest

  printf 'echo hi\\npwd\\n' | gbash

  IN THE INTERACTIVE DEMO
  -----------------------

  Try:
    pwd
    ls
    tree
    cat README.md
    cat go.mod
    sed -n '1,40p' cmd/gbash/version.go

  NOTE
  ----

  The browser shell is gbash.
  The optional agent route needs synced source data and ANTHROPIC_API_KEY.

  Enable JavaScript for the interactive terminal.
`;

export default function Home() {
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  return (
    <>
      <noscript>
        <pre>{NOSCRIPT_CONTENT}</pre>
      </noscript>
      <TerminalData />
      {mounted ? <TerminalComponent /> : null}
    </>
  );
}
