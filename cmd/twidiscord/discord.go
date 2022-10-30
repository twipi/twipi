package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/twikit/cmd/twidiscord/store"
	"github.com/diamondburned/twikit/logger"
	"github.com/diamondburned/twikit/twicli"
	"github.com/diamondburned/twikit/twipi"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
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

type storer interface {
	ChannelToSerial(context.Context, discord.UserID, discord.ChannelID) (int, error)
	SerialToChannel(context.Context, discord.UserID, int) (discord.ChannelID, error)
}

var _ storer = (*store.SQLite)(nil)

type handler struct {
	twipi    *twipi.ConfiguredServer
	accounts []*accountHandler
}

func bindHandler(twipisrv *twipi.ConfiguredServer, cfg *config, store storer) (*handler, error) {

	h := &handler{
		twipi:    twipisrv,
		accounts: make([]*accountHandler, 0, len(cfg.Discord.Accounts)),
	}

	for _, account := range cfg.Discord.Accounts {
		ah := newAccountHandler(twipisrv, account, store)
		ah.bind()
		h.accounts = append(h.accounts, ah)
	}

	return h, nil
}

// Connect connects all the accounts.
func (h *handler) Connect(ctx context.Context) error {
	ctx = logger.WithLogPrefix(ctx, "twidiscord: ")

	var errg errgroup.Group

	for i, account := range h.accounts {
		i := i
		account := account

		errg.Go(func() error {
			account.ctx = ctx
			account.ctx = logger.WithLogPrefix(account.ctx, string(account.UserNumber.String()))
			account.discord = account.discord.WithContext(account.ctx)

			log.Printf("connecting to Discord accounts[%d]", i)
			return account.discord.Connect(ctx)
		})
	}

	return errg.Wait()
}

func (h *handler) Command() twicli.Command {
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

func (h *handler) accountDispatcher(method func(*accountHandler, twicli.Message) error) twicli.ActionFunc {
	return func(_ context.Context, src twicli.Message) error {
		for _, account := range h.accounts {
			if account.UserNumber.String() == src.From {
				return method(account, src)
			}
		}

		// Just ignore this number.
		return nil
	}
}

type accountHandler struct {
	configAccount
	twipi   *twipi.ConfiguredServer
	discord *state.State
	store   storer

	fragmentMu sync.Mutex
	fragments  map[string]messageFragment

	ctx context.Context
}

func newAccountHandler(twipisrv *twipi.ConfiguredServer, account configAccount, store storer) *accountHandler {
	id := gateway.DefaultIdentifier(account.Token.String())
	id.Properties = gateway.IdentifyProperties{
		OS:      runtime.GOOS,
		Device:  fmt.Sprintf("twikit/%s", hostname),
		Browser: "twidiscord",
	}
	return &accountHandler{
		configAccount: account,
		twipi:         twipisrv,
		discord:       state.NewWithIdentifier(id),
		store:         store,
		fragments:     make(map[string]messageFragment),
		ctx:           context.Background(),
	}
}

type messageFragment struct {
	content string
}

func (h *accountHandler) bind() {
	h.discord.AddHandler(h.onMessageCreate)
	h.discord.AddHandler(h.onMessageUpdate)
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
		From: h.TwilioNumber.String(),
		To:   h.UserNumber.String(),
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
		"Discord, message <0> something here...\n"+
		"Discord, message <0> the first part (...)\n"+
		"Discord, message <0> the final part\n"+
		"Discord, message alieb something here...\n"+
		"Discord, help\n",
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
			if errors.Is(err, store.ErrNotFound) {
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
