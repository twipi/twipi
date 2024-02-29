package twipi

import (
	"context"

	"github.com/pkg/errors"
	"github.com/twilio/twilio-go"
	twilioapi "github.com/twilio/twilio-go/rest/api/v2010"
)

// RFC2822Date is the RFC 2822 date format.
const RFC2822Date = "Mon, 02 Jan 2006 15:04:05 -0700"

// ErrNoAccount is returned when no account is found for a given phone number.
var ErrNoAccount = errors.New("no account found associated with given input")

// Client is a Twilio client. A zero-value client is ready to use.
type Client struct {
	clients []AccountClient
}

type AccountClient struct {
	*twilio.RestClient
	Account Account
}

// Account represents a Twilio account.
type Account struct {
	PhoneNumber PhoneNumber
	AccountSID  string
	AuthToken   string
}

// NewClient creates a new Twilio client.
func NewClient() *Client {
	return &Client{}
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

// FromPhone returns the account associated with the given phone number.
func (c *Client) FromPhone(number PhoneNumber) *AccountClient {
	for i, client := range c.clients {
		if client.Account.PhoneNumber == number {
			return &c.clients[i]
		}
	}
	return nil
}

// SendSMS sends an SMS message to the given recipient. There is no guarantee
// that the message will be delivered.
func (c *Client) SendSMS(ctx context.Context, msg Message) error {
	client := c.FromPhone(msg.From)
	if client == nil {
		return ErrNoAccount
	}

	params := &twilioapi.CreateMessageParams{}
	params.SetTo(string(msg.To))
	params.SetFrom(string(msg.From))
	params.SetBody(msg.Body)
	params.SetSmartEncoded(true)

	_, err := client.Api.CreateMessage(params)
	if err != nil {
		return err
	}

	return nil
}

// ReplySMS replies to the given message.
func (c *Client) ReplySMS(ctx context.Context, msg Message, body string) error {
	reply := Message{
		From: msg.To,
		To:   msg.From,
		Body: body,
	}

	// Allow for opportunistic replies by checking if the message is accepting
	// replies. This usually allows us to reply to messages by writing TwiML.
	if msg.tryReply(reply) {
		return nil
	}

	// Fallback to using the API.
	return c.SendSMS(ctx, reply)
}
