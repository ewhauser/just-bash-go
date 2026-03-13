package commands

import "context"

type Realpath struct{}

func NewRealpath() *Realpath {
	return &Realpath{}
}

func (c *Realpath) Name() string {
	return "realpath"
}

func (c *Realpath) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Realpath)(nil)
