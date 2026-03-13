package commands

import "context"

type Nice struct{}

func NewNice() *Nice {
	return &Nice{}
}

func (c *Nice) Name() string {
	return "nice"
}

func (c *Nice) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Nice)(nil)
