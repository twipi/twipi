package module

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/diamondburned/twikit/cmd/twid/twid"
	"github.com/diamondburned/twikit/cmd/twid/twidiscord"
	"github.com/diamondburned/twikit/cmd/twid/twidiscord/store"
	"github.com/diamondburned/twikit/cmd/twid/twidiscord/web/routes"
	"github.com/diamondburned/twikit/logger"
	"github.com/diamondburned/twikit/twicli"
	"github.com/diamondburned/twikit/twipi"
	"github.com/pkg/errors"
)

// Handler is the main handler that binds Twipi and Discord.
type Handler struct {
	twipi  *twipi.ConfiguredServer
	config twidiscord.Config
	store  twidiscord.Storer

	accountMu sync.Mutex
	accounts  []*accountHandler

	wg  sync.WaitGroup
	ctx context.Context
}

var (
	_ twid.Handler        = (*Handler)(nil)
	_ twid.TwipiHandler   = (*Handler)(nil)
	_ twid.HTTPCommander  = (*Handler)(nil)
	_ twid.CommandHandler = (*Handler)(nil)
)

// NewHandler creates a new handler with the given twipi server and config.
func NewHandler(twipisrv *twipi.ConfiguredServer, cfg twidiscord.Config, store twidiscord.Storer) *Handler {
	return &Handler{
		twipi:  twipisrv,
		config: cfg,
		store:  store,
	}
}

// Config returns the local configuration instance for this module. It
// implements twid.Handler.
func (h *Handler) Config() any {
	return &h.config
}

// BindTwipi implements twid.TwipiBinder.
func (h *Handler) BindTwipi(twipisrv *twipi.ConfiguredServer) {
	h.twipi = twipisrv
}

// AddAccount adds an account to the handler. It will connect to the account
// immediately.
func (h *Handler) AddAccount(account twidiscord.Account) {
	ah := newAccountHandler(h.twipi, account, h.config, h.store)

	h.accountMu.Lock()
	defer h.accountMu.Unlock()

	for _, a := range h.accounts {
		if a.UserNumber == ah.UserNumber {
			return
		}
	}

	h.accounts = append(h.accounts, ah)

	if h.ctx != nil {
		h.startAccount(ah)
	}
}

func (h *Handler) startAccount(ah *accountHandler) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		ah.ctx = h.ctx
		ah.ctx = logger.WithLogPrefix(ah.ctx, "discord: "+string(ah.UserNumber))
		ah.discord = ah.discord.WithContext(ah.ctx)
		ah.bind()

		if err := ah.discord.Connect(ah.ctx); err != nil {
			log := logger.FromContext(ah.ctx)
			log.Printf("failed to connect to Discord for user %s: %v", ah.UserNumber, err)

			// Tell the user that we failed to connect.
			ah.twipi.Client.SendSMS(ah.ctx, twipi.Message{
				From: ah.TwilioNumber,
				To:   ah.UserNumber,
				Body: fmt.Sprintf("Sorry, we couldn't connect to Discord: %v", err),
			})
		}

		log := logger.FromContext(ah.ctx)
		log.Printf("disconnected from Discord for user %s", ah.UserNumber)
	}()
}

// Command implements twid.HandlerCommander.
func (h *Handler) Command() twicli.Command {
	return twicli.Command{
		Prefix: twicli.CombinePrefixes(
			twicli.NewSlashPrefix("discord"),
			twicli.NewNaturalPrefix("Discord"),
		),
		Action: twicli.Subcommands([]twicli.Command{
			{
				Prefix: twicli.NewWordPrefix("message", true),
				Action: h.accountDispatcher((*accountHandler).sendMessage),
			},
			{
				Prefix: twicli.NewWordPrefix("mute", true),
				Action: h.accountDispatcher((*accountHandler).sendMute),
			},
			{
				Prefix: twicli.NewWordPrefix("unmute", true),
				Action: h.accountDispatcher((*accountHandler).sendUnmute),
			},
			{
				Prefix: twicli.NewWordPrefix("help", true),
				Action: h.accountDispatcher((*accountHandler).sendHelp),
			},
			{
				Prefix: twicli.NewWordPrefix("summarize", true),
				Action: h.accountDispatcher((*accountHandler).sendSummarize),
			},
		}),
	}
}

// HTTPHandler implements twid.HTTPCommander.
func (h *Handler) HTTPHandler() http.Handler {
	return routes.Mount(h.twipi, h.config, (*accountAdder)(h))
}

// HTTPPrefix implements twid.HTTPCommander.
func (h *Handler) HTTPPrefix() string {
	return "/discord"
}

type accountAdder Handler

func (a *accountAdder) AddAccount(ctx context.Context, account twidiscord.Account) error {
	if err := a.store.SetAccount(ctx, account); err != nil {
		return err
	}

	(*Handler)(a).AddAccount(account)
	return nil
}

func (h *Handler) accountDispatcher(method func(*accountHandler, twicli.Message) error) twicli.ActionFunc {
	return func(_ context.Context, src twicli.Message) error {
		for _, account := range h.accounts {
			if account.UserNumber == src.From {
				return method(account, src)
			}
		}

		// Just ignore this number.
		return nil
	}
}

// Start connects all the accounts. It blocks until ctx is canceled.
func (m *Handler) Start(ctx context.Context) error {
	db, err := store.Open(ctx, m.config.Discord.DatabaseURI.String(), false)
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer store.Close(db)

	m.ctx = logger.WithLogPrefix(ctx, "twidiscord:")
	m.store = db

	// wg should block until ctx returns.
	m.wg.Add(1)
	go func() {
		<-ctx.Done()
		m.wg.Done()
	}()

	// Start existing accounts.
	m.accountMu.Lock()
	for _, account := range m.accounts {
		m.startAccount(account)
	}
	m.accountMu.Unlock()

	accounts, err := m.store.Accounts(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load accounts")
	}

	for _, account := range accounts {
		m.AddAccount(account)
	}

	m.wg.Wait()
	return nil
}
