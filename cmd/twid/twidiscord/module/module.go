package module

import (
	"context"

	"github.com/diamondburned/twikit/cmd/twid/twid"
)

func init() {
	twid.Register(Module)
}

// Module is the twidiscord module.
var Module = twid.Module{
	Name: "discord",
	New: func() twid.Handler {
		return &Handler{ctx: context.Background()}
	},
}
