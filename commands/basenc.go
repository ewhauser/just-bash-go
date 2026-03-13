package commands

import "context"

type Basenc struct{}

func NewBasenc() *Basenc {
	return &Basenc{}
}

func (c *Basenc) Name() string {
	return "basenc"
}

func (c *Basenc) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Basenc)(nil)
