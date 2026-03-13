package commands

import "context"

type Stty struct{}

func NewStty() *Stty {
	return &Stty{}
}

func (c *Stty) Name() string {
	return "stty"
}

func (c *Stty) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Stty)(nil)
