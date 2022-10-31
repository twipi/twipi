package twipi

import (
	"github.com/diamondburned/twikit/internal/cfgutil"
	"github.com/pkg/errors"
)

// Config is the primary config for Twipi webhook handlers. Pair it with a
// configuration file of choice. The primary supported languages are JSON and
// TOML.
type Config struct {
	// Secrets is the secret section of Config. It contains sensitive
	// information such as the Twilio account SID and auth token. It is
	// strongly discouraged to store this information in a regular config
	// file. Instead, use environment variables or a separate, more
	// protected file.
	Accounts []ConfigAccount
	Webhook  struct {
		Message struct {
			Enable           bool   `toml:"enable" json:"enable"`
			IncomingEndpoint string `toml:"incoming_endpoint" json:"incoming_endpoint"`
			DeliveryEndpoint string `toml:"delivery_endpoint" json:"delivery_endpoint"`
		} `toml:"message" json:"message"`
	} `toml:"webhook" json:"webhook"`
}

// ConfigAccount is an account config block.
type ConfigAccount struct {
	PhoneNumber cfgutil.Env[PhoneNumber] `toml:"phone_number" json:"phone_number"`
	AccountSID  cfgutil.EnvString        `toml:"account_sid" json:"account_sid"`
	AuthToken   cfgutil.EnvString        `toml:"auth_token" json:"auth_token"`
}

// Value returns c as the Account type.
func (c ConfigAccount) Value() Account {
	return Account{
		PhoneNumber: c.PhoneNumber.Value(),
		AccountSID:  c.AccountSID.String(),
		AuthToken:   c.AuthToken.String(),
	}
}

// ConfiguredServer contains servers initialized from a Config. Handlers that
// are disabled will be nil. The WebhookServer will always be non-nil.
type ConfiguredServer struct {
	*WebhookRouter
	Client  *Client // API client
	Message *MessageHandler
}

// NewConfiguredServer creates a new ConfiguredServer from a Config.
func NewConfiguredServer(c Config) (*ConfiguredServer, error) {
	if len(c.Accounts) == 0 {
		return nil, errors.New("no accounts given")
	}

	s := ConfiguredServer{
		WebhookRouter: NewWebhookRouter(),
		Client:        NewClient(),
	}

	for _, account := range c.Accounts {
		s.Client.AddAccount(account.Value())
	}

	if c.Webhook.Message.Enable {
		cfg := c.Webhook.Message
		s.Message = NewMessageHandler(cfg.IncomingEndpoint, cfg.DeliveryEndpoint)
		s.RegisterWebhook(s.Message)
	}

	return &s, nil
}

// NewConfiguredServerFromPath creates a new ConfiguredServer from a config file
// path. The file extension is used to determine the config format.
func NewConfiguredServerFromPath(path string) (*ConfiguredServer, error) {
	c, err := cfgutil.ParseFile[Config](path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse config file")
	}

	return NewConfiguredServer(*c)
}
