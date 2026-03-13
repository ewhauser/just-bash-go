package commands

import "context"

type Truncate struct{}

func NewTruncate() *Truncate {
	return &Truncate{}
}

func (c *Truncate) Name() string {
	return "truncate"
}

func (c *Truncate) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Truncate)(nil)
