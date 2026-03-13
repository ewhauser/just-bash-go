package commands

import "context"

// TODO: When implementing chgrp, reuse permission_helpers.go for traversal,
// identity lookup, and ownership-change reporting instead of duplicating chown logic.

type Chgrp struct{}

func NewChgrp() *Chgrp {
	return &Chgrp{}
}

func (c *Chgrp) Name() string {
	return "chgrp"
}

func (c *Chgrp) Run(_ context.Context, inv *Invocation) error {
	return runNotImplemented(inv, c.Name())
}

var _ Command = (*Chgrp)(nil)
