package commands

import "context"

type Mkfifo struct{}

func NewMkfifo() *Mkfifo {
	return &Mkfifo{}
}

func (c *Mkfifo) Name() string {
	return "mkfifo"
}

func (c *Mkfifo) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Mkfifo)(nil)
