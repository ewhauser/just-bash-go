package commands

import "context"

type Unlink struct{}

func NewUnlink() *Unlink {
	return &Unlink{}
}

func (c *Unlink) Name() string {
	return "unlink"
}

func (c *Unlink) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Unlink)(nil)
