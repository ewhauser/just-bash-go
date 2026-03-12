package commands

import (
	"context"
	"fmt"
	"strings"
)

type TR struct{}

type trOptions struct {
	complement bool
	delete     bool
	squeeze    bool
}

type trSet struct {
	ordered []byte
	index   map[byte]int
	present [256]bool
}

func NewTR() *TR {
	return &TR{}
}

func (c *TR) Name() string {
	return "tr"
}

func (c *TR) Run(_ context.Context, inv *Invocation) error {
	opts, set1Text, set2Text, err := parseTRArgs(inv)
	if err != nil {
		return err
	}

	set1, err := parseTRSet(set1Text)
	if err != nil {
		return exitf(inv, 1, "tr: %v", err)
	}
	set2, err := parseTRSet(set2Text)
	if err != nil {
		return exitf(inv, 1, "tr: %v", err)
	}

	data, err := readAllStdin(inv)
	if err != nil {
		return err
	}
	out := make([]byte, 0, len(data))
	var (
		lastOut byte
		hasLast bool
	)
	squeezeMembership := chooseTRSqueezeMembership(opts, &set1, &set2)
	replacementFallback := byte(0)
	if len(set2.ordered) > 0 {
		replacementFallback = set2.ordered[len(set2.ordered)-1]
	}

	for _, current := range data {
		matched := set1.contains(current)
		if opts.complement {
			matched = !matched
		}

		emit := true
		switch {
		case opts.delete && matched:
			emit = false
		case len(set2.ordered) > 0 && matched:
			if opts.complement {
				current = replacementFallback
			} else {
				current = set2.translate(&set1, current)
			}
		}

		if !emit {
			continue
		}
		if opts.squeeze && hasLast && lastOut == current && squeezeMembership(current) {
			continue
		}
		out = append(out, current)
		lastOut = current
		hasLast = true
	}

	if _, err := inv.Stdout.Write(out); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}

func parseTRArgs(inv *Invocation) (opts trOptions, set1, set2 string, err error) {
	args := inv.Args
	for len(args) > 0 {
		arg := args[0]
		if arg == "--" {
			args = args[1:]
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			break
		}
		switch arg {
		case "-c", "--complement":
			opts.complement = true
			args = args[1:]
			continue
		case "-C":
			opts.complement = true
			args = args[1:]
			continue
		case "-d", "--delete":
			opts.delete = true
			args = args[1:]
			continue
		case "-s", "--squeeze-repeats":
			opts.squeeze = true
			args = args[1:]
			continue
		}
		for _, flag := range arg[1:] {
			switch flag {
			case 'c', 'C':
				opts.complement = true
			case 'd':
				opts.delete = true
			case 's':
				opts.squeeze = true
			default:
				return trOptions{}, "", "", exitf(inv, 1, "tr: unsupported flag -%c", flag)
			}
		}
		args = args[1:]
	}

	if len(args) == 0 {
		return trOptions{}, "", "", exitf(inv, 1, "tr: missing operand")
	}
	set1 = args[0]
	args = args[1:]
	if len(args) > 0 {
		set2 = args[0]
		args = args[1:]
	}
	if len(args) > 0 {
		return trOptions{}, "", "", exitf(inv, 1, "tr: extra operand %q", args[0])
	}
	if !opts.delete && !opts.squeeze && set2 == "" {
		return trOptions{}, "", "", exitf(inv, 1, "tr: missing second operand")
	}
	if opts.delete && !opts.squeeze && set2 != "" {
		return trOptions{}, "", "", exitf(inv, 1, "tr: extra operand %q", set2)
	}
	return opts, set1, set2, nil
}

func parseTRSet(value string) (trSet, error) {
	set := trSet{
		index: make(map[byte]int),
	}
	if value == "" {
		return set, nil
	}
	data := []byte(value)
	for i := 0; i < len(data); {
		if data[i] == '[' && i+1 < len(data) && data[i+1] == ':' {
			end := i + 2
			for end+1 < len(data) && (data[end] != ':' || data[end+1] != ']') {
				end++
			}
			if end+1 >= len(data) {
				return trSet{}, fmt.Errorf("invalid character class %q", value[i:])
			}
			classBytes, err := trClassBytes(string(data[i+2 : end]))
			if err != nil {
				return trSet{}, err
			}
			for _, b := range classBytes {
				set.append(b)
			}
			i = end + 2
			continue
		}

		left, advance, err := parseTRByte(data, i)
		if err != nil {
			return trSet{}, err
		}
		next := i + advance
		if next+1 < len(data) && data[next] == '-' {
			right, rightAdvance, err := parseTRByte(data, next+1)
			if err == nil && left <= right {
				for current := left; current <= right; current++ {
					set.append(current)
				}
				i = next + 1 + rightAdvance
				continue
			}
		}
		set.append(left)
		i = next
	}
	return set, nil
}

func parseTRByte(data []byte, index int) (value byte, advance int, err error) {
	if data[index] != '\\' {
		return data[index], 1, nil
	}
	if index+1 >= len(data) {
		return '\\', 1, nil
	}
	switch data[index+1] {
	case 'n':
		return '\n', 2, nil
	case 'r':
		return '\r', 2, nil
	case 't':
		return '\t', 2, nil
	case '\\':
		return '\\', 2, nil
	case '0':
		end := index + 2
		for end < len(data) && end < index+5 && isOctalDigit(data[end]) {
			end++
		}
		value, err := parseOctalByte(data[index+1 : end])
		return value, end - index, err
	default:
		if isOctalDigit(data[index+1]) {
			end := index + 1
			for end < len(data) && end < index+4 && isOctalDigit(data[end]) {
				end++
			}
			value, err := parseOctalByte(data[index+1 : end])
			return value, end - index, err
		}
		return data[index+1], 2, nil
	}
}

func parseOctalByte(data []byte) (byte, error) {
	var value byte
	for _, b := range data {
		if !isOctalDigit(b) {
			return 0, fmt.Errorf("invalid octal escape")
		}
		value = (value * 8) + (b - '0')
	}
	return value, nil
}

func isOctalDigit(b byte) bool {
	return b >= '0' && b <= '7'
}

func trClassBytes(name string) ([]byte, error) {
	var out []byte
	for b := 0; b <= 0x7f; b++ {
		switch name {
		case "alnum":
			if isASCIIAlpha(byte(b)) || isASCIIDigit(byte(b)) {
				out = append(out, byte(b))
			}
		case "alpha":
			if isASCIIAlpha(byte(b)) {
				out = append(out, byte(b))
			}
		case "digit":
			if isASCIIDigit(byte(b)) {
				out = append(out, byte(b))
			}
		case "lower":
			if b >= 'a' && b <= 'z' {
				out = append(out, byte(b))
			}
		case "space":
			if isASCIISpace(byte(b)) {
				out = append(out, byte(b))
			}
		case "upper":
			if b >= 'A' && b <= 'Z' {
				out = append(out, byte(b))
			}
		default:
			return nil, fmt.Errorf("unsupported character class %q", name)
		}
	}
	return out, nil
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isASCIISpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func (s *trSet) append(b byte) {
	if _, exists := s.index[b]; !exists {
		s.index[b] = len(s.ordered)
	}
	s.ordered = append(s.ordered, b)
	s.present[b] = true
}

func (s *trSet) contains(b byte) bool {
	return s.present[b]
}

func (s *trSet) translate(source *trSet, b byte) byte {
	if len(s.ordered) == 0 {
		return b
	}
	position, ok := source.index[b]
	if !ok {
		return s.ordered[len(s.ordered)-1]
	}
	if position >= len(s.ordered) {
		position = len(s.ordered) - 1
	}
	return s.ordered[position]
}

func chooseTRSqueezeMembership(opts trOptions, set1, set2 *trSet) func(byte) bool {
	switch {
	case opts.squeeze && len(set2.ordered) > 0 && !opts.delete:
		return set2.contains
	case opts.squeeze && len(set2.ordered) > 0 && opts.delete:
		return set2.contains
	default:
		return set1.contains
	}
}

var _ Command = (*TR)(nil)
