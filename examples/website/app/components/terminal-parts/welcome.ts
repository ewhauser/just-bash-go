import { ASCII_ART } from "./constants";

type Terminal = {
  write: (data: string) => void;
  writeln: (data: string) => void;
  cols: number;
};

export function showWelcome(term: Terminal) {
  term.writeln("");

  // Only show ASCII art if terminal is wide enough (43+ chars)
  if (term.cols >= 43) {
    for (const line of ASCII_ART) {
      term.writeln(line);
    }
  } else {
    term.writeln("\x1b[1mgbash (WASM)\x1b[0m");
    term.writeln("============");
  }
  term.writeln("");

  term.writeln("\x1b[2mgbash running in-browser via WebAssembly inside a vendored terminal UI.\x1b[0m");
  term.writeln("");
  term.writeln("  \x1b[1m\x1b[36mgo install github.com/ewhauser/gbash/cmd/gbash@latest\x1b[0m");
  term.writeln("");
  term.writeln("\x1b[2m  gb, err := gbash.New()\x1b[0m");
  term.writeln("\x1b[2m  result, err := gb.Run(ctx, &gbash.ExecutionRequest{\x1b[0m");
  term.writeln("\x1b[2m    Script: 'echo hello\\npwd\\n',\x1b[0m");
  term.writeln("\x1b[2m  })\x1b[0m");
  term.writeln("");
  term.writeln(
    "\x1b[2mCommands:\x1b[0m \x1b[36mabout\x1b[0m, \x1b[36minstall\x1b[0m, \x1b[36mgithub\x1b[0m, \x1b[36mhelp\x1b[0m"
  );
  term.writeln(
    "\x1b[2mTry:\x1b[0m \x1b[36mpwd\x1b[0m, \x1b[36mtree\x1b[0m, \x1b[36mcat\x1b[0m go.mod, \x1b[36msed\x1b[0m -n '1,40p' cmd/gbash/version.go, \x1b[36mgrep\x1b[0m -n WithHTTPAccess README.md"
  );
  term.writeln(
    "\x1b[2mLocal note:\x1b[0m the optional \x1b[36magent\x1b[0m route needs synced source data and \x1b[36mANTHROPIC_API_KEY\x1b[0m."
  );
  term.writeln("");
  term.write("$ ");
}
