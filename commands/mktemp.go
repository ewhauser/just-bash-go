package commands

import "context"

type Mktemp struct{}

func NewMktemp() *Mktemp {
	return &Mktemp{}
}

func (c *Mktemp) Name() string {
	return "mktemp"
}

func (c *Mktemp) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Mktemp)(nil)
