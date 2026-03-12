package commands

import (
	"context"
	"fmt"
	"strings"
)

type Whoami struct{}

func NewWhoami() *Whoami {
	return &Whoami{}
}

func (c *Whoami) Name() string {
	return "whoami"
}

func (c *Whoami) Run(_ context.Context, inv *Invocation) error {
	args := append([]string(nil), inv.Args...)
	for _, arg := range args {
		switch {
		case arg == "--help":
			_, _ = fmt.Fprintln(inv.Stdout, "usage: whoami")
			return nil
		case arg == "--version":
			_, _ = fmt.Fprintln(inv.Stdout, "whoami (gbash)")
			return nil
		case strings.HasPrefix(arg, "--"):
			return exitf(inv, 1, "whoami: unrecognized option '%s'", arg)
		case strings.HasPrefix(arg, "-"):
			return exitf(inv, 1, "whoami: invalid option -- '%s'", strings.TrimPrefix(arg, "-"))
		default:
			return exitf(inv, 1, "whoami: extra operand '%s'\nTry 'whoami --help' for more information.", arg)
		}
	}

	_, err := fmt.Fprintln(inv.Stdout, idCurrentIdentity(inv).userName)
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}

var _ Command = (*Whoami)(nil)
