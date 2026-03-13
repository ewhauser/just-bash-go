package commands

import "context"

type Shred struct{}

func NewShred() *Shred {
	return &Shred{}
}

func (c *Shred) Name() string {
	return "shred"
}

func (c *Shred) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Shred)(nil)
