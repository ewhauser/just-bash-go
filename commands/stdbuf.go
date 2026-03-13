package commands

import "context"

type Stdbuf struct{}

func NewStdbuf() *Stdbuf {
	return &Stdbuf{}
}

func (c *Stdbuf) Name() string {
	return "stdbuf"
}

func (c *Stdbuf) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Stdbuf)(nil)
