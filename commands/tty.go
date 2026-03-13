package commands

import "context"

type Tty struct{}

func NewTty() *Tty {
	return &Tty{}
}

func (c *Tty) Name() string {
	return "tty"
}

func (c *Tty) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Tty)(nil)
