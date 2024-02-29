package twilio

import (
	"github.com/twipi/cfgutil"
	"github.com/twipi/twipi/twisms"
)

// Config is the primary config for Twipi webhook handlers. Pair it with a
// configuration file of choice. The primary supported languages are JSON and
// TOML.
type Config struct {
	Accounts []ConfigAccount
	Message  struct {
		Enable  bool `toml:"enable" json:"enable"`
		Webhook struct {
			IncomingEndpoint string `toml:"incoming_endpoint" json:"incoming_endpoint"`
			DeliveryEndpoint string `toml:"delivery_endpoint" json:"delivery_endpoint"`
		} `toml:"webhook" json:"webhook"`
	} `toml:"message" json:"message"`
}

// ConfigAccount is an account config block.
type ConfigAccount struct {
	PhoneNumber cfgutil.Env[twisms.PhoneNumber] `toml:"phone_number" json:"phone_number"`
	AccountSID  cfgutil.EnvString               `toml:"account_sid" json:"account_sid"`
	AuthToken   cfgutil.EnvString               `toml:"auth_token" json:"auth_token"`
	BaseURL     cfgutil.EnvString               `json:"base_url" toml:"base_url"`
	ManagedName string                          `json:"managed_name" toml:"managed_name"`
	Override    bool                            `json:"override" toml:"override"`
}

// Value returns c as the Account type.
func (c ConfigAccount) Value() Account {
	return Account{
		PhoneNumber: c.PhoneNumber.Value(),
		AccountSID:  c.AccountSID.String(),
		AuthToken:   c.AuthToken.String(),
	}
}
