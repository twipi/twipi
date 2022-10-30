package main

import (
	"github.com/diamondburned/twikit/internal/cfgutil"
	"github.com/diamondburned/twikit/twipi"
)

type config struct {
	twipi.Config

	Discord struct {
		Database cfgutil.EnvString `toml:"database" json:"database"`
		Accounts []configAccount   `toml:"accounts" json:"accounts"`
	} `toml:"discord" json:"discord"`
}

type configAccount struct {
	Token        cfgutil.EnvString              `toml:"token" json:"token"`
	TwilioNumber cfgutil.Env[twipi.PhoneNumber] `toml:"twilio_number" json:"twilio_number"`
	UserNumber   cfgutil.Env[twipi.PhoneNumber] `toml:"user_number" json:"user_number"`
}
