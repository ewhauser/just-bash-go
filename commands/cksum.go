package commands

import "context"

type Cksum struct{}

func NewCksum() *Cksum {
	return &Cksum{}
}

func (c *Cksum) Name() string {
	return "cksum"
}

func (c *Cksum) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Cksum)(nil)
