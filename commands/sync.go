package commands

import "context"

type Sync struct{}

func NewSync() *Sync {
	return &Sync{}
}

func (c *Sync) Name() string {
	return "sync"
}

func (c *Sync) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Sync)(nil)
