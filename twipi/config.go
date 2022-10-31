package twipi

import (
	"context"
	"net/url"

	"github.com/diamondburned/twikit/internal/cfgutil"
	"github.com/diamondburned/twikit/logger"
	"github.com/pkg/errors"

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

// UpdateTwilio updates the Twilio Messaging services to work with Twipi. This
// function does not return any errors; they will simply be logged.
func (c *ConfiguredServer) UpdateTwilio(ctx context.Context) {
	for _, account := range c.Config.Accounts {
		ctx := logger.WithLogPrefix(ctx, "twipi: populateMessageService: "+account.PhoneNumber.String())
		client := c.Client.FromPhone(account.PhoneNumber.Value())
		populateMessageServiceAccount(ctx, client, c.Config, account)
	}
}

func populateMessageServiceAccount(
	ctx context.Context,
	client *AccountClient,
	cfg Config,
	cfgAccount ConfigAccount) {

	if !cfgAccount.Override {
		return
	}

	log := logger.FromContext(ctx)

	var incomingURL *string
	var deliveryURL *string

	if cfg.Message.Webhook.IncomingEndpoint != "" {
		u, _ := url.Parse(cfgAccount.BaseURL.String())
		u.Path = cfg.Message.Webhook.IncomingEndpoint
		incomingURL = vptr(u.String())
	}

	if cfg.Message.Webhook.DeliveryEndpoint != "" {
		u, _ := url.Parse(cfgAccount.BaseURL.String())
		u.Path = cfg.Message.Webhook.DeliveryEndpoint
		deliveryURL = vptr(u.String())
	}

	var twipiSID string
	var createdService bool

	services, errs := client.MessagingV1.StreamService(nil)
	for service := range services {
		if nilz(service.FriendlyName) != "twipi" {
			continue
		}

		if nilz(service.InboundRequestUrl) == nilz(incomingURL) &&
			nilz(service.StatusCallback) == nilz(deliveryURL) {
			// Found.
			twipiSID = nilz(service.Sid)
			goto checkNumber
		}

		// Found a service named twipi, but it's not the
		// right one. We'll update that one.
		twipiSID = nilz(service.Sid)
		goto createService
	}

	if err := <-errs; err != nil {
		log.Println("failed to stream services:", err)
		return
	}

createService:
	createdService = true

	// Not found, create a new one.
	if twipiSID == "" {
		v, err := client.MessagingV1.CreateService(&twiliomessaging.CreateServiceParams{
			FriendlyName:              vptr("twipi"),
			InboundMethod:             vptr("POST"),
			InboundRequestUrl:         incomingURL,
			StatusCallback:            deliveryURL,
			UseInboundWebhookOnNumber: vptr(false),
		})
		if err != nil {
			log.Println("failed to create messaging service:", err)
			return
		}
		twipiSID = nilz(v.Sid)
	} else {
		_, err := client.MessagingV1.UpdateService(twipiSID, &twiliomessaging.UpdateServiceParams{
			FriendlyName:              vptr("twipi"),
			InboundMethod:             vptr("POST"),
			InboundRequestUrl:         incomingURL,
			StatusCallback:            deliveryURL,
			UseInboundWebhookOnNumber: vptr(false),
		})
		if err != nil {
			log.Println("failed to update messaging service:", err)
			return
		}
	}

checkNumber:
	// Check that this service has the right numbers.
	serviceNumbers, errs := client.MessagingV1.StreamPhoneNumber(twipiSID, nil)
	for number := range serviceNumbers {
		if nilz(number.PhoneNumber) == string(client.Account.PhoneNumber) {
			if createdService {
				log.Println("successfully set up service (created new service)")
			}
			return
		}
	}

	if err := <-errs; err != nil {
		log.Println("failed to stream service numbers:", err)
		return
	}

	var numberSID string

	numbers, errs := client.Api.StreamIncomingPhoneNumber(nil)
	for number := range numbers {
		if nilz(number.PhoneNumber) == string(client.Account.PhoneNumber) {
			numberSID = nilz(number.Sid)
			break
		}
	}

	if err := <-errs; err != nil {
		log.Println("failed to stream known numbers:", err)
		return
	}

	if numberSID == "" {
		log.Println("number not found in Twilio")
		return
	}

	// Set the number to use the service.
	_, err := client.MessagingV1.CreatePhoneNumber(twipiSID, &twiliomessaging.CreatePhoneNumberParams{
		PhoneNumberSid: vptr(numberSID),
	})
	if err != nil {
		log.Println("failed to set number to use service:", err)
		return
	}

	log.Println("successfully set up service")
}

func vptr[T any](s T) *T {
	return &s
}

func nilz[T any](s *T) T {
	if s == nil {
		var z T
		return z
	}
	return *s
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
