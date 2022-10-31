package module

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/ws"
	"github.com/diamondburned/twikit/cmd/twid/twid"
	"github.com/diamondburned/twikit/cmd/twid/twidiscord"
	"github.com/diamondburned/twikit/cmd/twid/twidiscord/store"
	"github.com/diamondburned/twikit/cmd/twid/twidiscord/web/routes"
	"github.com/diamondburned/twikit/logger"
	"github.com/diamondburned/twikit/twicli"
	"github.com/diamondburned/twikit/twipi"
	"github.com/pkg/errors"
)

var hostname string

func init() {
	h, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	} else {
		hostname = h
	}
}

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
		ctx:    context.Background(),
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
	ah := newAccountHandler(h.twipi, account, h.store)

	h.accountMu.Lock()
	defer h.accountMu.Unlock()

	for _, a := range h.accounts {
		if a.UserNumber == ah.UserNumber {
			return
		}
	}

	h.accounts = append(h.accounts, ah)

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		ah.ctx = h.ctx
		ah.ctx = logger.WithLogPrefix(ah.ctx, string(ah.UserNumber))
		ah.discord = ah.discord.WithContext(ah.ctx)
		ah.bind()

		if err := ah.discord.Connect(h.ctx); err != nil {
			log := logger.FromContext(ah.ctx)
			log.Printf("failed to connect to Discord for user %s: %v", ah.UserNumber, err)

			// Tell the user that we failed to connect.
			ah.twipi.Client.SendSMS(ah.ctx, twipi.Message{
				From: ah.TwilioNumber,
				To:   ah.UserNumber,
				Body: fmt.Sprintf("Sorry, we couldn't connect to Discord: %v", err),
			})
		}
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
				Prefix: twicli.NewWordPrefix("help", true),
				Action: h.accountDispatcher((*accountHandler).sendHelp),
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

type accountHandler struct {
	twidiscord.Account
	twipi   *twipi.ConfiguredServer
	discord *state.State
	store   twidiscord.Storer

	fragmentMu sync.Mutex
	fragments  map[string]messageFragment

	ctx context.Context
}

func newAccountHandler(twipisrv *twipi.ConfiguredServer, account twidiscord.Account, store twidiscord.Storer) *accountHandler {
	id := gateway.DefaultIdentifier(account.DiscordToken)
	id.Properties = gateway.IdentifyProperties{
		OS:      runtime.GOOS,
		Device:  fmt.Sprintf("twikit/%s", hostname),
		Browser: "twidiscord",
	}

	return &accountHandler{
		Account:   account,
		twipi:     twipisrv,
		discord:   state.NewWithIdentifier(id),
		store:     store,
		fragments: make(map[string]messageFragment),
		ctx:       context.Background(),
	}
}

type messageFragment struct {
	content string
}

func (h *accountHandler) bind() {
	var tag string
	h.discord.AddHandler(h.onMessageCreate)
	h.discord.AddHandler(h.onMessageUpdate)
	h.discord.AddHandler(func(*gateway.ReadyEvent) {
		me, _ := h.discord.Me()
		tag = me.Tag()

		log := logger.FromContext(h.ctx)
		log.Printf("connected to Discord account %q", tag)
	})
	h.discord.AddHandler(func(closeEv *ws.CloseEvent) {
		log := logger.FromContext(h.ctx)
		log.Printf("disconnected from Discord account %q (code %d)", tag, closeEv.Code)
	})
}

func (h *accountHandler) onMessageCreate(ev *gateway.MessageCreateEvent) {
	h.onMessage(&ev.Message, false)
}

func (h *accountHandler) onMessageUpdate(ev *gateway.MessageUpdateEvent) {
	h.onMessage(&ev.Message, true)
}

func (h *accountHandler) onMessage(msg *discord.Message, edited bool) {
	// Guild or bot messages are not supported.
	if msg.Author.Bot || msg.GuildID.IsValid() {
		return
	}

	// Ignore messages sent by the user themselves.
	me, _ := h.discord.Me()
	if me == nil || msg.Author.ID == me.ID {
		return
	}

	serial, err := h.store.ChannelToSerial(h.ctx, me.ID, msg.ChannelID)
	if err != nil {
		log := logger.FromContext(h.ctx)
		log.Printf("twidiscord: failed to get serial for %s: %v", msg.ChannelID, err)
		return
	}

	body := fmt.Sprintf("<%d>%s: %s", serial, msg.Author.Username, msg.Content)
	if edited {
		body += " (edited)"
	}

	err = h.twipi.Client.SendSMS(h.ctx, twipi.Message{
		From: h.TwilioNumber,
		To:   h.UserNumber,
		Body: body,
	})
	if err != nil {
		log := logger.FromContext(h.ctx)
		log.Println("cannot send SMS on message:", err)
		return
	}
}

func (h *accountHandler) sendHelp(src twicli.Message) error {
	return h.twipi.Client.ReplySMS(h.ctx, src.Message, "Usages:\n"+
		"Discord, message <0> content\n"+
		"Discord, message <0> the first part (...)\n"+
		"Discord, message <0> the final part\n"+
		"Discord, message alieb Hello!\n"+
		"Discord, help",
	)
}

var (
	tagSerialRe = regexp.MustCompile(`<(\d+)>`)
	// tagChIDRe   = regexp.MustCompile(`<#(\d+)>`)
	// tagUserIDRe = regexp.MustCompile(`<@!?(\d+)>`)
)

func (h *accountHandler) sendMessage(src twicli.Message) error {
	ref, content, err := twicli.PopFirstWord(src.Body)
	if err != nil {
		return err
	}

	if strings.HasSuffix(content, "(...)") {
		if strings.HasSuffix(content, `\(...)`) {
			// The user escaped the ellipsis. Remove the escape.
			content = strings.TrimSuffix(content, `\(...)`) + "(...)"
		} else {
			// Store the message as a fragment.
			h.fragmentMu.Lock()
			h.fragments[ref] = messageFragment{
				content: content,
			}
			h.fragmentMu.Unlock()
			return nil
		}
	} else {
		// Check for previous fragments.
		h.fragmentMu.Lock()
		frag, ok := h.fragments[ref]
		if ok {
			content = frag.content + content
			delete(h.fragments, ref)
		}
		h.fragmentMu.Unlock()
	}

	chID, err := h.matchChReference(ref)
	if err != nil {
		return err
	}

	_, err = h.discord.SendMessage(chID, content)
	if err != nil {
		return err
	}

	return nil
}

func (h *accountHandler) matchChReference(str string) (discord.ChannelID, error) {
	me, err := h.discord.Me()
	if err != nil {
		return 0, err
	}

	if matches := tagSerialRe.FindStringSubmatch(str); matches != nil {
		n, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, errors.Wrap(err, "invalid serial")
		}

		chID, err := h.store.SerialToChannel(h.ctx, me.ID, n)
		if err != nil {
			if errors.Is(err, twidiscord.ErrNotFound) {
				return 0, errors.New("no such serial")
			}
			return 0, errors.Wrap(err, "failed to lookup given serial")
		}

		return chID, nil
	}

	dms, err := h.discord.PrivateChannels()
	if err == nil {
		if err := twicli.ValidatePattern(str); err != nil {
			return 0, errors.Wrap(err, "invalid channel name")
		}

		ch := matchDM(dms, str)
		if ch == nil {
			return 0, errors.New("no such channel")
		}

		return ch.ID, nil
	}

	return 0, errors.New("unknown channel reference given")
}

func matchDM(dms []discord.Channel, str string) *discord.Channel {
	for i, dm := range dms {
		if false ||
			(dm.Name != "" && twicli.PatternMatch(dm.Name, str)) ||
			(len(dm.DMRecipients) == 1 && twicli.PatternMatch(dm.DMRecipients[0].Username, str)) {

			return &dms[i]
		}
	}

	return nil
}
