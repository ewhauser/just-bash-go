package commands

import "context"

type Mknod struct{}

func NewMknod() *Mknod {
	return &Mknod{}
}

func (c *Mknod) Name() string {
	return "mknod"
}

func (c *Mknod) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Mknod)(nil)
