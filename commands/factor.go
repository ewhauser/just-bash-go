package commands

import "context"

type Factor struct{}

func NewFactor() *Factor {
	return &Factor{}
}

func (c *Factor) Name() string {
	return "factor"
}

func (c *Factor) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Factor)(nil)
