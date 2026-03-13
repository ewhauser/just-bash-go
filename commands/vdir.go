package commands

import "context"

type Vdir struct{}

func NewVdir() *Vdir {
	return &Vdir{}
}

func (c *Vdir) Name() string {
	return "vdir"
}

func (c *Vdir) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Vdir)(nil)
