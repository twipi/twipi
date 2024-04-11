package twilio

import (
	"context"
	"fmt"
	"net/url"

	"github.com/pkg/errors"
	"github.com/twilio/twilio-go"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twisms"

	twilioapi "github.com/twilio/twilio-go/rest/api/v2010"
)

// AccountClient is a Twilio client associated with a Twilio account.
type AccountClient struct {
	*twilio.RestClient
	Account Account
}

// Account represents a Twilio account.
type Account struct {
	PhoneNumber string
	AccountSID  string
	AuthToken   string
}

// MessageSender is a Twilio API wrapper for sending messages.
// A zero-value client is ready to use but won't send any messages.
type MessageSender struct {
	clients []AccountClient
}

var _ twisms.MessageSender = (*MessageSender)(nil)

// NewMessageSender creates a new Twilio sender client.
func NewMessageSender() *MessageSender {
	return &MessageSender{}
}

// NewMessageSenderFromConfig creates a new Twilio sender client from a Config.
func NewMessageSenderFromConfig(cfg Config) (*MessageSender, error) {
	if len(cfg.Accounts) == 0 {
		return nil, errors.New("no accounts in config")
	}

	var c MessageSender
	for _, account := range cfg.Accounts {
		if account.BaseURL.String() == "" && account.Override {
			return nil, errors.New("base_url is required when override is true")
		}

		if account.BaseURL.String() != "" {
			_, err := url.Parse(account.BaseURL.String())
			if err != nil {
				return nil, errors.Wrapf(err, "invalid base URL for %s", account.PhoneNumber)
			}
		}

		c.AddAccount(account.Value())
	}

	return &c, nil
}

// AddAccount adds a Twilio account to the client.
func (c *MessageSender) AddAccount(account Account) {
	c.clients = append(c.clients, AccountClient{
		RestClient: twilio.NewRestClientWithParams(twilio.ClientParams{
			Username: account.AccountSID,
			Password: account.AuthToken,
		}),
		Account: account,
	})
}

// fromPhone returns the account associated with the given phone number.
func (c *MessageSender) fromPhone(number string) *AccountClient {
	for i, client := range c.clients {
		if client.Account.PhoneNumber == number {
			return &c.clients[i]
		}
	}
	return nil
}

// SendMessage sends an SMS message to the given recipient. There is no
// guarantee that the message will be delivered.
func (c *MessageSender) SendMessage(ctx context.Context, msg *twismsproto.Message) error {
	client := c.fromPhone(msg.From)
	if client == nil {
		return ErrNoAccount
	}

	params := &twilioapi.CreateMessageParams{}
	params.SetFrom(msg.From)
	params.SetTo(msg.To)

	if msg.Body.Text != nil {
		params.SetBody(msg.Body.Text.Text)
		params.SetSmartEncoded(true)
	}

	_, err := client.Api.CreateMessage(params)
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}

	return nil
}
