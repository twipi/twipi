package twipi

import (
	"context"

	"github.com/twilio/twilio-go"
	twilioapi "github.com/twilio/twilio-go/rest/api/v2010"
)

// RFC2822Date is the RFC 2822 date format.
const RFC2822Date = "Mon, 02 Jan 2006 15:04:05 -0700"

// Client is a Twilio client.
type Client struct {
	*twilio.RestClient
}

// NewClient creates a new Twilio client.
func NewClient(accountSID, authToken string) *Client {
	return &Client{
		RestClient: twilio.NewRestClientWithParams(twilio.ClientParams{
			Username: accountSID,
			Password: authToken,
		}),
	}
}

// SendSMS sends an SMS message to the given recipient. There is no guarantee
// that the message will be delivered.
func (c *Client) SendSMS(ctx context.Context, msg Message) error {
	params := &twilioapi.CreateMessageParams{}
	params.SetTo(string(msg.To))
	params.SetFrom(string(msg.From))
	params.SetBody(msg.Body)
	params.SetSmartEncoded(true)

	_, err := c.Api.CreateMessage(params)
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
