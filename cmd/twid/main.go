package main

import (
	"github.com/diamondburned/twikit/cmd/twid/twid"

	_ "github.com/diamondburned/twikit/cmd/twid/twidiscord/module"
)

func main() {
	twid.Main()
}
