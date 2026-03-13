package commands

import "context"

type Arch struct{}

func NewArch() *Arch {
	return &Arch{}
}

func (c *Arch) Name() string {
	return "arch"
}

func (c *Arch) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Arch)(nil)
