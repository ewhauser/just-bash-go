package commands

import "context"

type Sum struct{}

func NewSum() *Sum {
	return &Sum{}
}

func (c *Sum) Name() string {
	return "sum"
}

func (c *Sum) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Sum)(nil)
