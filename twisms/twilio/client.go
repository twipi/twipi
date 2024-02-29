package twilio

import (
	"context"
	"net/url"

	"github.com/pkg/errors"
	"github.com/twilio/twilio-go"
	"github.com/twipi/twipi/twisms"

	twilioapi "github.com/twilio/twilio-go/rest/api/v2010"
)

// ErrNoAccount is returned when no account is found for a given phone number.
var ErrNoAccount = errors.New("no account found associated with given input")

// AccountClient is a Twilio client associated with a Twilio account.
type AccountClient struct {
	*twilio.RestClient
	Account Account
}

// Account represents a Twilio account.
type Account struct {
	PhoneNumber twisms.PhoneNumber
	AccountSID  string
	AuthToken   string
}

// Client is a Twilio client.
// A zero-value client is ready to use.
type Client struct {
	clients []AccountClient
}

var _ twisms.MessageSender = (*Client)(nil)

// NewClient creates a new Twilio client.
func NewClient() *Client {
	return &Client{}
}

// NewClientFromConfig creates a new Twilio client from a Config.
func NewClientFromConfig(cfg Config) (*Client, error) {
	if len(cfg.Accounts) == 0 {
		return nil, errors.New("no accounts in config")
	}

	var c Client
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
func (c *Client) AddAccount(account Account) {
	c.clients = append(c.clients, AccountClient{
		RestClient: twilio.NewRestClientWithParams(twilio.ClientParams{
			Username: account.AccountSID,
			Password: account.AuthToken,
		}),
		Account: account,
	})
}

// fromPhone returns the account associated with the given phone number.
func (c *Client) fromPhone(number twisms.PhoneNumber) *AccountClient {
	for i, client := range c.clients {
		if client.Account.PhoneNumber == number {
			return &c.clients[i]
		}
	}
	return nil
}

// SendSMS sends an SMS message to the given recipient. There is no guarantee
// that the message will be delivered.
func (c *Client) SendSMS(ctx context.Context, msg twisms.Message) error {
	client := c.fromPhone(msg.From())
	if client == nil {
		return ErrNoAccount
	}

	textBody, ok := msg.Body().(twisms.MessageBodyText)
	if !ok {
		return twisms.ErrNonTextMessageBody
	}

	params := &twilioapi.CreateMessageParams{}
	params.SetTo(string(msg.To()))
	params.SetFrom(string(msg.From()))
	params.SetBody(string(textBody))
	params.SetSmartEncoded(true)

	_, err := client.Api.CreateMessage(params)
	if err != nil {
		return err
	}

	return nil
}

// ReplySMS replies to the given message.
func (c *Client) ReplySMS(ctx context.Context, msg twisms.Message, body twisms.MessageBody) error {
	replyText, ok := body.(twisms.MessageBodyText)
	if !ok {
		return twisms.ErrNonTextMessageBody
	}

	reply := SMS{
		from: msg.To(),
		to:   msg.From(),
		body: string(replyText),
	}

	if msg, ok := msg.(SMS); ok {
		// Allow for opportunistic replies by checking if the message is accepting
		// replies. This usually allows us to reply to messages by writing TwiML.
		if msg.tryReply(reply) {
			return nil
		}
	}

	// Fallback to using the API.
	return c.SendSMS(ctx, reply)
}
