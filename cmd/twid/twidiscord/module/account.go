package module

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/ws"
	"github.com/diamondburned/ningen/v3"
	"github.com/diamondburned/twikit/cmd/twid/twidiscord"
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

type accountHandler struct {
	twidiscord.Account
	twipi   *twipi.ConfiguredServer
	discord *ningen.State
	config  twidiscord.Config
	store   twidiscord.Storer

	fragmentMu sync.Mutex
	fragments  map[string]messageFragment

	sessions struct {
		sync.Mutex
		ourID    string
		sessions []gateway.UserSession
	}

	messageThrottlers messageThrottlers

	ctx context.Context
}

func newAccountHandler(twipisrv *twipi.ConfiguredServer, account twidiscord.Account, cfg twidiscord.Config, store twidiscord.Storer) *accountHandler {
	id := gateway.DefaultIdentifier(account.DiscordToken)
	id.Presence = &gateway.UpdatePresenceCommand{
		Status: discord.IdleStatus,
		AFK:    true,
	}
	id.Properties = gateway.IdentifyProperties{
		OS:      runtime.GOOS,
		Device:  fmt.Sprintf("twikit/%s", hostname),
		Browser: "twidiscord",
	}

	h := &accountHandler{
		Account:   account,
		twipi:     twipisrv,
		discord:   ningen.FromState(state.NewWithIdentifier(id)),
		config:    cfg,
		store:     store,
		fragments: make(map[string]messageFragment),
		ctx:       context.Background(),
	}

	h.messageThrottlers = *newMessageThrottlers(messageThrottleConfig{
		max: 15,
		do:  h.sendMessageIDs,
	})

	return h
}

type messageFragment struct {
	content string
}

func (h *accountHandler) bind() {
	h.discord.AddHandler(h.onMessageCreate)
	h.discord.AddHandler(h.onMessageUpdate)
	h.discord.AddHandler(h.onTypingStart)

	var tag string
	h.discord.AddHandler(func(r *gateway.ReadyEvent) {
		me, _ := h.discord.Me()
		tag = me.Tag()

		h.sessions.Lock()
		h.sessions.sessions = r.Sessions
		h.sessions.ourID = r.SessionID
		h.sessions.Unlock()

		log := logger.FromContext(h.ctx)
		log.Printf("connected to Discord account %q", tag)
	})

	h.discord.AddHandler(func(closeEv *ws.CloseEvent) {
		log := logger.FromContext(h.ctx)
		log.Printf("disconnected from Discord account %q (code %d)", tag, closeEv.Code)
	})

	h.discord.AddHandler(func(err error) {
		log := logger.FromContext(h.ctx)
		log.Printf("non-fatal error from Discord account %q: %v", tag, err)
	})

	h.discord.AddHandler(func(sessions *gateway.SessionsReplaceEvent) {
		h.sessions.Lock()
		h.sessions.sessions = []gateway.UserSession(*sessions)
		h.sessions.Unlock()
	})

	// h.bindDebug()
}

func (h *accountHandler) bindDebug() {
	ws.EnableRawEvents = true

	os.MkdirAll("/tmp/twidiscord-events", os.ModePerm)

	var serial uint64
	h.discord.AddHandler(func(ev *ws.RawEvent) {
		if ev.OriginalType != "SESSIONS_REPLACE" {
			return
		}

		b, err := json.Marshal(ev)
		if err != nil {
			return
		}

		n := atomic.AddUint64(&serial, 1)
		if err := os.WriteFile(fmt.Sprintf("/tmp/twidiscord-events/%s-%d.json", ev.OriginalType, n), b, os.ModePerm); err != nil {
			return
		}
	})
}

// hasOtherSessions returns true if the current user has other sessions opened
// right now.
func (h *accountHandler) hasOtherSessions() bool {
	h.sessions.Lock()
	defer h.sessions.Unlock()

	for _, session := range h.sessions.sessions {
		// Ignore our session or idle sessions.
		if session.SessionID == h.sessions.ourID || session.Status == discord.IdleStatus {
			continue
		}
		return true
	}

	return false
}

func (h *accountHandler) isValidChannel(chID discord.ChannelID) bool {
	// Check if the channel is muted. Ignore muted channels.
	return !h.discord.ChannelIsMuted(chID, true)
}

func (h *accountHandler) isValidMessage(msg *discord.Message) bool {
	me, _ := h.discord.Cabinet.Me()
	if me == nil {
		return false
	}

	// Ignore messages sent by the current user or a bot.
	if msg.Author.ID == me.ID || msg.Author.Bot {
		return false
	}

	if msg.GuildID.IsValid() {
		return false
	}

	return true
}

func (h *accountHandler) onMessageCreate(ev *gateway.MessageCreateEvent) {
	if !h.isValidChannel(ev.ChannelID) || !h.isValidMessage(&ev.Message) {
		return
	}

	throttler := h.messageThrottlers.forChannel(ev.ChannelID)
	throttler.AddMessage(ev.ID, 5*time.Second)
}

func (h *accountHandler) onMessageUpdate(ev *gateway.MessageUpdateEvent) {
	if !h.isValidChannel(ev.ChannelID) {
		return
	}

	msg, _ := h.discord.Cabinet.Message(ev.ChannelID, ev.ID)
	if msg == nil || !h.isValidMessage(msg) {
		return
	}

	throttler := h.messageThrottlers.forChannel(ev.ChannelID)
	throttler.AddMessage(ev.ID, 5*time.Second)
}

func (h *accountHandler) onTypingStart(ev *gateway.TypingStartEvent) {
	if !h.isValidChannel(ev.ChannelID) {
		return
	}

	throttler := h.messageThrottlers.forChannel(ev.ChannelID)
	throttler.DelaySending(10 * time.Second)
}

func (h *accountHandler) sendMessageIDs(chID discord.ChannelID, ids []discord.MessageID) {
	if len(ids) == 0 {
		return
	}

	// Check if we're muted or if we have any existing Discord sessions.
	if h.hasOtherSessions() || h.store.NumberIsMuted(h.ctx, h.TwilioNumber) {
		return
	}

	if !h.isValidChannel(chID) {
		return
	}

	// Ignore all of our efforts in keeping track of a list of IDs. We'll
	// actually just grab the earliest ID in this list.
	earliest := ids[0]

	msgs, err := h.discord.Messages(chID, 100)
	if err != nil {
		return
	}

	filtered := msgs[:0]
	for i, msg := range msgs {
		if msg.ID >= earliest && h.isValidMessage(&msgs[i]) {
			filtered = append(filtered, msg)
		}
	}

	if len(filtered) == 0 {
		return
	}

	me, _ := h.discord.Cabinet.Me()
	if me == nil {
		return
	}

	serial, err := h.store.ChannelToSerial(h.ctx, me.ID, chID)
	if err != nil {
		log := logger.FromContext(h.ctx)
		log.Printf("twidiscord: failed to get serial for %s: %v", chID, err)
		return
	}

	var body strings.Builder
	fmt.Fprintf(&body, "^%d: %s: ", serial, filtered[0].Author.Tag())

	// Iterate from earliest.
	for i := len(filtered) - 1; i >= 0; i-- {
		msg := &filtered[i]
		body.WriteString(renderText(h.ctx, h.discord, msg.Content, msg))

		if len(msg.Embeds) > 0 {
			if len(msg.Embeds) == 1 {
				body.WriteString("\n[1 embed]")
			} else {
				fmt.Fprintf(&body, "\n[%d embeds]", len(msg.Embeds))
			}
		}

		for _, attachment := range msg.Attachments {
			fmt.Fprintf(&body, "\n[attached %s]", attachment.Filename)
		}

		if msg.EditedTimestamp.IsValid() {
			body.WriteString("*")
		}

		body.WriteByte('\n')
	}

	err = h.twipi.Client.SendSMS(h.ctx, twipi.Message{
		From: h.TwilioNumber,
		To:   h.UserNumber,
		Body: strings.TrimSuffix(body.String(), "\n"),
	})
	if err != nil {
		log := logger.FromContext(h.ctx)
		log.Println("cannot send SMS on message:", err)
		return
	}
}

func (h *accountHandler) sendHelp(src twicli.Message) error {
	return h.twipi.Client.ReplySMS(h.ctx, src.Message, "Usages:\n"+
		"Discord, message ^0 content\n"+
		"Discord, message ^0 the first part (...)\n"+
		"Discord, message ^0 the final part\n"+
		"Discord, message alieb Hello!\n"+
		"Discord, summarize\n"+
		"Discord, mute\n"+
		"Discord, unmute\n"+
		"Discord, help",
	)
}

func (h *accountHandler) sendMute(src twicli.Message) error {
	if err := h.store.SetNumberMuted(h.ctx, h.TwilioNumber, true); err != nil {
		return err
	}

	return h.twipi.Client.ReplySMS(h.ctx, src.Message,
		"Muted. No more messages will be sent from Discord.")
}

func (h *accountHandler) sendUnmute(src twicli.Message) error {
	if err := h.store.SetNumberMuted(h.ctx, h.TwilioNumber, false); err != nil {
		return err
	}

	return h.twipi.Client.ReplySMS(h.ctx, src.Message,
		"Unmuted. You will receive messages again.")
}

func (h *accountHandler) sendSummarize(src twicli.Message) error {
	dms, err := h.discord.Cabinet.PrivateChannels()
	if err != nil {
		return err
	}

	sort.Slice(dms, func(i, j int) bool {
		return dms[i].LastMessageID < dms[j].LastMessageID
	})

	type unreadChannel struct {
		discord.Channel
		MentionCount int
	}

	var unreads []unreadChannel

	for _, dm := range dms {
		if h.discord.ChannelIsMuted(dm.ID, true) {
			continue
		}

		readState := h.discord.ReadState.ReadState(dm.ID)
		if readState == nil || !readState.LastMessageID.IsValid() {
			continue
		}

		if readState.MentionCount == 0 {
			continue
		}

		unreads = append(unreads, unreadChannel{
			Channel:      dm,
			MentionCount: readState.MentionCount,
		})
	}

	if len(unreads) == 0 {
		return h.twipi.Client.ReplySMS(h.ctx, src.Message, "No unread messages.")
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "You have %d unread channels:\n", len(unreads))
	for _, unread := range unreads {
		fmt.Fprintf(&buf, "%s: %d\n", chName(unread.Channel), unread.MentionCount)
	}

	return h.twipi.Client.ReplySMS(h.ctx, src.Message, buf.String())
}

var (
	tagSerialRe = regexp.MustCompile(`^\^(\d+)$`)
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

	dms, err := h.discord.Cabinet.PrivateChannels()
	if err != nil {
		return 0, errors.Wrap(err, "failed to list private channels")
	}

	if err := twicli.ValidatePattern(str); err != nil {
		return 0, errors.Wrap(err, "invalid channel name")
	}

	ch := matchDM(dms, str)
	if ch == nil {
		return 0, errors.New("no such channel")
	}

	return ch.ID, nil
}

func matchDM(dms []discord.Channel, str string) *discord.Channel {
	for i, dm := range dms {
		if twicli.PatternMatch(chName(dm), str) {
			return &dms[i]
		}
	}

	return nil
}

func chName(ch discord.Channel) string {
	if ch.Name != "" {
		return ch.Name
	}

	if len(ch.DMRecipients) == 1 {
		return ch.DMRecipients[0].Username
	}

	return ""
}
