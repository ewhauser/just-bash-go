package commands

import "context"

type Runcon struct{}

func NewRuncon() *Runcon {
	return &Runcon{}
}

func (c *Runcon) Name() string {
	return "runcon"
}

func (c *Runcon) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Runcon)(nil)
