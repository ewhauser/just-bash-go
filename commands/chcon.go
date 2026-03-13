package commands

import "context"

type Chcon struct{}

func NewChcon() *Chcon {
	return &Chcon{}
}

func (c *Chcon) Name() string {
	return "chcon"
}

func (c *Chcon) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Chcon)(nil)
