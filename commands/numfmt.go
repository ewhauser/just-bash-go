package commands

import "context"

type Numfmt struct{}

func NewNumfmt() *Numfmt {
	return &Numfmt{}
}

func (c *Numfmt) Name() string {
	return "numfmt"
}

func (c *Numfmt) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Numfmt)(nil)
