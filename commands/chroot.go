package commands

import "context"

type Chroot struct{}

func NewChroot() *Chroot {
	return &Chroot{}
}

func (c *Chroot) Name() string {
	return "chroot"
}

func (c *Chroot) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Chroot)(nil)
