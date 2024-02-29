package twipi

import (
	"context"
	"log"
	"log/slog"
	"net/url"
	"slices"

	"github.com/pkg/errors"
	"github.com/twipi/twikit/internal/cfgutil"
	"github.com/twipi/twikit/internal/slogctx"

	twilioapi "github.com/twilio/twilio-go/rest/api/v2010"
	twiliomessaging "github.com/twilio/twilio-go/rest/messaging/v1"
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
	PhoneNumber cfgutil.Env[PhoneNumber] `toml:"phone_number" json:"phone_number"`
	AccountSID  cfgutil.EnvString        `toml:"account_sid" json:"account_sid"`
	AuthToken   cfgutil.EnvString        `toml:"auth_token" json:"auth_token"`
	BaseURL     cfgutil.EnvString        `json:"base_url" toml:"base_url"`
	ManagedName string                   `json:"managed_name" toml:"managed_name"`
	Override    bool                     `json:"override" toml:"override"`
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
	Config  Config
	Client  *Client // API client
	Message *MessageHandler
}

// NewConfiguredServer creates a new ConfiguredServer from a Config.
func NewConfiguredServer(c Config) (*ConfiguredServer, error) {
	if len(c.Accounts) == 0 {
		return nil, errors.New("no accounts in config")
	}

	s := ConfiguredServer{
		WebhookRouter: NewWebhookRouter(),
		Config:        c,
		Client:        NewClient(),
	}

	for _, account := range c.Accounts {
		if account.BaseURL.String() == "" && account.Override {
			return nil, errors.New("base_url is required when override is true")
		}

		if account.BaseURL.String() != "" {
			_, err := url.Parse(account.BaseURL.String())
			if err != nil {
				return nil, errors.Wrapf(err, "invalid base URL for %s", account.PhoneNumber)
			}
		}

		s.Client.AddAccount(account.Value())
	}

	if c.Message.Enable {
		wcfg := c.Message.Webhook
		s.Message = NewMessageHandler(wcfg.IncomingEndpoint, wcfg.DeliveryEndpoint)
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

// UpdateTwilio updates the Twilio Messaging services to work with Twipi. This
// function does not return any errors; they will simply be logged.
func (c *ConfiguredServer) UpdateTwilio(ctx context.Context) {
	for _, account := range c.Config.Accounts {
		client := c.Client.FromPhone(account.PhoneNumber.Value())
		logger := slogctx.From(ctx).With(
			"account.account_sid", account.AccountSID,
			"account.phone_number", account.PhoneNumber.String(),
		)

		populateMessageServiceAccount(ctx, client, logger, c.Config, account)
	}
}

func populateMessageServiceAccount(
	ctx context.Context,
	client *AccountClient,
	logger *slog.Logger,
	cfg Config,
	cfgAccount ConfigAccount) {

	friendlyName := "twipi"
	if cfgAccount.ManagedName != "" {
		friendlyName = cfgAccount.ManagedName
	}

	logger = logger.With(
		"account.friendly_name", friendlyName,
		"action", "populateMessageServiceAccount",
	)

	if !cfgAccount.Override {
		logger.DebugContext(ctx, "skipping account as configured")
		return
	}

	var incomingURL *string
	var deliveryURL *string

	if cfg.Message.Webhook.IncomingEndpoint != "" {
		u, _ := url.Parse(cfgAccount.BaseURL.String())
		u.Path = cfg.Message.Webhook.IncomingEndpoint
		incomingURL = ptrTo(u.String())
	}

	if cfg.Message.Webhook.DeliveryEndpoint != "" {
		u, _ := url.Parse(cfgAccount.BaseURL.String())
		u.Path = cfg.Message.Webhook.DeliveryEndpoint
		deliveryURL = ptrTo(u.String())
	}

	services, err := client.MessagingV1.ListService(nil)
	if err != nil {
		slog.ErrorContext(ctx,
			"failed to list MessagingV1 services",
			"err", err)
		return
	}

	service := findFunc(services, func(s twiliomessaging.MessagingV1Service) bool {
		return deref(s.FriendlyName) == friendlyName
	})
	if service == nil {
		v, err := client.MessagingV1.CreateService(&twiliomessaging.CreateServiceParams{
			FriendlyName:              ptrTo(friendlyName),
			InboundMethod:             ptrTo("POST"),
			InboundRequestUrl:         incomingURL,
			StatusCallback:            deliveryURL,
			UseInboundWebhookOnNumber: ptrTo(false),
		})
		if err != nil {
			slog.ErrorContext(ctx,
				"failed to create messaging service",
				"err", err)
			return
		}
		service = v
	}

	if service.Sid == nil {
		slog.ErrorContext(ctx,
			"service.Sid is unexpectedly nil")
		return
	}

	if service != nil && (false ||
		deref(service.InboundRequestUrl) != deref(incomingURL) ||
		deref(service.StatusCallback) != deref(deliveryURL)) {

		_, err := client.MessagingV1.UpdateService(*service.Sid, &twiliomessaging.UpdateServiceParams{
			FriendlyName:              ptrTo("twipi"),
			InboundMethod:             ptrTo("POST"),
			InboundRequestUrl:         incomingURL,
			StatusCallback:            deliveryURL,
			UseInboundWebhookOnNumber: ptrTo(false),
		})
		if err != nil {
			log.Println("failed to update messaging service:", err)
			return
		}
	}

	// Check that this service has the right numbers.
	serviceNumbers, err := client.MessagingV1.ListPhoneNumber(*service.Sid, nil)
	if err != nil {
		slog.ErrorContext(ctx,
			"failed to list phone numbers for messaging service",
			"err", err)
		return
	}

	serviceNumber := findFunc(serviceNumbers, func(n twiliomessaging.MessagingV1PhoneNumber) bool {
		return deref(n.PhoneNumber) == string(client.Account.PhoneNumber)
	})
	if serviceNumber == nil {
		numbers, err := client.Api.ListIncomingPhoneNumber(nil)
		if err != nil {
			slog.ErrorContext(ctx,
				"failed to list incoming phone numbers",
				"err", err)
			return
		}

		number := findFunc(numbers, func(number twilioapi.ApiV2010IncomingPhoneNumber) bool {
			return deref(number.PhoneNumber) == string(client.Account.PhoneNumber)
		})
		if number == nil {
			slog.ErrorContext(ctx,
				"number not found in Twilio",
				"incoming_phone_numbers", len(numbers))
			return
		}

		// Set the number to use the service.
		_, err = client.MessagingV1.CreatePhoneNumber(*service.Sid, &twiliomessaging.CreatePhoneNumberParams{
			PhoneNumberSid: number.Sid,
		})
		if err != nil {
			log.Println("failed to set number to use service:", err)
			return
		}
	}

	slog.Info("successfully set up service")
}

func ptrTo[T any](s T) *T {
	return &s
}

func deref[T any](s *T) T {
	if s == nil {
		var z T
		return z
	}
	return *s
}

func findFunc[T any](items []T, f func(T) bool) *T {
	if i := slices.IndexFunc(items, f); i != -1 {
		return &items[i]
	}
	return nil
}
