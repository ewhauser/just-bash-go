package commands

import (
	"context"
	"fmt"
	"io"
	"strings"
)

type Dirname struct{}

func NewDirname() *Dirname {
	return &Dirname{}
}

func (c *Dirname) Name() string {
	return "dirname"
}

func (c *Dirname) Run(_ context.Context, inv *Invocation) error {
	args := append([]string(nil), inv.Args...)
	terminator := "\n"

	for len(args) > 0 {
		arg := args[0]
		switch {
		case arg == "--":
			args = args[1:]
			goto operands
		case arg == "--zero":
			terminator = "\x00"
			args = args[1:]
		case arg == "--help":
			_, _ = io.WriteString(inv.Stdout, dirnameHelpText)
			return nil
		case arg == "--version":
			_, _ = io.WriteString(inv.Stdout, dirnameVersionText)
			return nil
		case arg == "-":
			goto operands
		case strings.HasPrefix(arg, "-"):
			rest := arg[1:]
			if rest == "" {
				goto operands
			}
			args = args[1:]
			for rest != "" {
				switch rest[0] {
				case 'z':
					terminator = "\x00"
					rest = rest[1:]
				default:
					return exitf(inv, 1, "dirname: unsupported flag -%c", rest[0])
				}
			}
		default:
			goto operands
		}
	}

operands:
	if len(args) == 0 {
		return exitf(inv, 1, "dirname: missing operand\nTry 'dirname --help' for more information.")
	}

	for _, arg := range args {
		if _, err := fmt.Fprint(inv.Stdout, dirnameString(arg), terminator); err != nil {
			return &ExitError{Code: 1, Err: err}
		}
	}
	return nil
}

func dirnameString(name string) string {
	if name == "" {
		return "."
	}

	end := len(name)
	allSlashes := true
	for i := 0; i < end; i++ {
		if name[i] != '/' {
			allSlashes = false
			break
		}
	}
	if allSlashes {
		return "/"
	}

	for end > 1 && name[end-1] == '/' {
		end--
	}

	if end >= 2 && name[end-1] == '.' && name[end-2] == '/' {
		slashStart := end - 2
		for slashStart > 0 && name[slashStart-1] == '/' {
			slashStart--
		}
		if slashStart == 0 {
			if name[0] == '/' {
				return "/"
			}
			return "."
		}
		return name[:slashStart]
	}

	lastSlash := -1
	for i := end - 1; i >= 0; i-- {
		if name[i] == '/' {
			lastSlash = i
			break
		}
	}
	if lastSlash < 0 {
		return "."
	}

	result := name[:lastSlash]
	for len(result) > 1 && result[len(result)-1] == '/' {
		result = result[:len(result)-1]
	}
	if result == "" {
		return "/"
	}
	return result
}

const dirnameHelpText = `Usage: dirname NAME...
  or:  dirname OPTION NAME...
Output each NAME with its last non-slash component and trailing slashes removed.
`

const dirnameVersionText = `dirname (gbash)
`

var _ Command = (*Dirname)(nil)
