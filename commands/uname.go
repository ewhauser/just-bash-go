package commands

import "context"

type Uname struct{}

func NewUname() *Uname {
	return &Uname{}
}

func (c *Uname) Name() string {
	return "uname"
}

func (c *Uname) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Uname)(nil)
