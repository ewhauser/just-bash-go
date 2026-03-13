package commands

import "context"

type Tsort struct{}

func NewTsort() *Tsort {
	return &Tsort{}
}

func (c *Tsort) Name() string {
	return "tsort"
}

func (c *Tsort) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Tsort)(nil)
