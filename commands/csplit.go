package commands

import "context"

type Csplit struct{}

func NewCsplit() *Csplit {
	return &Csplit{}
}

func (c *Csplit) Name() string {
	return "csplit"
}

func (c *Csplit) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Csplit)(nil)
