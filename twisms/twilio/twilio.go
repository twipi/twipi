// Package twilop contains basic abstractions around some Twilio APIs. It's
// designed to easily allow Twilio to be used in a Go application by providing
// high-level abstractions around the API.
//
// If you're making a Twilio application that can handle replying to commands,
// also consider using the twicli package.
package twilio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"

	"libdb.so/ctxt"

	twilioapi "github.com/twilio/twilio-go/rest/api/v2010"
	twiliomessaging "github.com/twilio/twilio-go/rest/messaging/v1"
)

// InitializeTwilio updates the Twilio Messaging services to work with Twipi.
// It uses the given Client to update the services as specified in the given
// Config.
func InitializeTwilio(ctx context.Context, c *Client, cfg Config) error {
	var errs []error
	for _, account := range cfg.Accounts {
		client := c.fromPhone(account.PhoneNumber.Value())
		logger := ctxt.FromOrFunc(ctx, slog.Default).With(
			"account.twilio_sid", account.AccountSID,
			"account.phone_number", account.PhoneNumber.String(),
			"account.managed_name", account.ManagedName,
			"action", "populateMessageServiceAccount",
		)
		if err := populateMessageServiceAccount(ctx, client, logger, cfg, account); err != nil {
			logger.ErrorContext(ctx,
				"could not populate message service account",
				"err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func populateMessageServiceAccount(
	ctx context.Context,
	client *AccountClient,
	logger *slog.Logger,
	cfg Config,
	cfgAccount ConfigAccount) error {

	friendlyName := "twipi"
	if cfgAccount.ManagedName != "" {
		friendlyName = cfgAccount.ManagedName
	}

	if !cfgAccount.Override {
		logger.InfoContext(ctx, "skipping account as configured")
		return nil
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
		return fmt.Errorf("failed to list MessagingV1 services: %w", err)
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
			return fmt.Errorf("failed to create messaging service: %w", err)
		}
		service = v
	}

	if service.Sid == nil {
		return fmt.Errorf("service.Sid is unexpectedly nil")
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
			return fmt.Errorf("failed to update messaging service: %w", err)
		}
	}

	// Check that this service has the right numbers.
	serviceNumbers, err := client.MessagingV1.ListPhoneNumber(*service.Sid, nil)
	if err != nil {
		return fmt.Errorf("failed to list phone numbers for messaging service: %w", err)
	}

	serviceNumber := findFunc(serviceNumbers, func(n twiliomessaging.MessagingV1PhoneNumber) bool {
		return deref(n.PhoneNumber) == string(client.Account.PhoneNumber)
	})
	if serviceNumber == nil {
		numbers, err := client.Api.ListIncomingPhoneNumber(nil)
		if err != nil {
			return fmt.Errorf("failed to list incoming phone numbers: %w", err)
		}

		number := findFunc(numbers, func(number twilioapi.ApiV2010IncomingPhoneNumber) bool {
			return deref(number.PhoneNumber) == string(client.Account.PhoneNumber)
		})
		if number == nil {
			slog.WarnContext(ctx,
				"configured number not found in Twilio",
				"incoming_phone_numbers", len(numbers))
			return fmt.Errorf("configured number not found in Twilio")
		}

		// Set the number to use the service.
		_, err = client.MessagingV1.CreatePhoneNumber(*service.Sid, &twiliomessaging.CreatePhoneNumberParams{
			PhoneNumberSid: number.Sid,
		})
		if err != nil {
			return fmt.Errorf("failed to set number to use service: %w", err)
		}
	}

	slog.Info("successfully set up service")
	return nil
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
