package commands

import "context"

type Who struct{}

func NewWho() *Who {
	return &Who{}
}

func (c *Who) Name() string {
	return "who"
}

func (c *Who) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Who)(nil)
