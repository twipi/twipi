package routes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"sync"

	"github.com/diamondburned/twikit/cmd/twidiscord/twidiscord"
	"github.com/diamondburned/twikit/cmd/twidiscord/web"
	"github.com/diamondburned/twikit/twipi"
	"github.com/go-chi/chi/v5"
	"github.com/pkg/errors"
)

var registerTmpl = web.Templates.Register("register", "routes/register.html")

const registerTokenCookie = "twidiscord-register-token"

// AccountAdder is a function that adds an account to the database.
type AccountAdder interface {
	AddAccount(ctx context.Context, account twidiscord.Account) error
}

type registerHandler struct {
	*chi.Mux

	twipi        *twipi.ConfiguredServer
	cfg          *twidiscord.Config
	accountAdder AccountAdder

	confirmationMu sync.Mutex
	confirmations  map[string]confirmationData
}

type confirmationData struct {
	registerPostData
	ConfirmationCode string
}

func newRegisterHandler(
	twipi *twipi.ConfiguredServer,
	cfg *twidiscord.Config,
	accountAdder AccountAdder) *registerHandler {

	h := &registerHandler{
		Mux:           chi.NewMux(),
		twipi:         twipi,
		cfg:           cfg,
		accountAdder:  accountAdder,
		confirmations: make(map[string]confirmationData),
	}

	r := h.Mux
	r.Get("/", h.get)
	r.Post("/", h.post)
	r.Route("/confirm", func(r chi.Router) {
		r.Get("/", h.confirmGET)
		r.Post("/", h.confirmPOST)
	})

	return h
}

func (h *registerHandler) get(w http.ResponseWriter, r *http.Request) {
	registerTmpl.Execute(w, nil)
}

type registerPostData struct {
	UserNumber   twipi.PhoneNumber
	TwilioNumber twipi.PhoneNumber
	DiscordToken string
}

func (h *registerHandler) post(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(registerTokenCookie)
	if cookie != nil {
		// Delete our confirmation.
		h.confirmationMu.Lock()
		delete(h.confirmations, cookie.Value)
		h.confirmationMu.Unlock()
	}

	if err := r.ParseForm(); err != nil {
		renderError(w, http.StatusBadRequest, err)
		return
	}

	data := registerPostData{
		UserNumber:   twipi.PhoneNumber(r.FormValue("user-number")),
		TwilioNumber: twipi.PhoneNumber(r.FormValue("twilio-number")),
		DiscordToken: r.FormValue("discord-token"),
	}

	if !h.twilioNumberKnown(data.TwilioNumber) {
		renderError(w, http.StatusBadRequest,
			fmt.Errorf("unknown Twilio number %q given", data.TwilioNumber))
		return
	}

	if !h.userNumberKnown(data.UserNumber) {
		renderError(w, http.StatusBadRequest,
			fmt.Errorf("unknown user number %q given", data.UserNumber))
		return
	}

	confirmationCode := generateConfirmationCode()

	if err := h.twipi.Client.SendSMS(r.Context(), twipi.Message{
		From: data.TwilioNumber,
		To:   data.UserNumber,
		Body: fmt.Sprintf(
			"Hi, it's twidiscord! Your confirmation code is %s. "+
				"If you didn't request this, please ignore this message.",
			confirmationCode,
		),
	}); err != nil {
		renderError(w, http.StatusInternalServerError, errors.Wrap(err, "cannot send SMS"))
		return
	}

	confirmationToken := h.addConfirmation(confirmationData{
		registerPostData: data,
		ConfirmationCode: confirmationCode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:  registerTokenCookie,
		Value: confirmationToken,
		Path:  "/discord/register",
	})

	http.Redirect(w, r, "/discord/register/confirm", http.StatusSeeOther)
}

func (h *registerHandler) twilioNumberKnown(number twipi.PhoneNumber) bool {
	for _, n := range h.cfg.Discord.KnownNumbers {
		if n.Value() == number {
			return true
		}
	}
	return false
}

func (h *registerHandler) userNumberKnown(number twipi.PhoneNumber) bool {
	if h.cfg.Discord.AllowedUsers == nil {
		return true
	}
	for _, n := range h.cfg.Discord.AllowedUsers {
		if n.Value() == number {
			return true
		}
	}
	return false
}

func (h *registerHandler) addConfirmation(data confirmationData) string {
	var buf [24]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			panic(err)
		}

		token := base64.URLEncoding.EncodeToString(buf[:])

		h.confirmationMu.Lock()
		_, ok := h.confirmations[token]
		if ok {
			h.confirmationMu.Unlock()
			continue
		}
		h.confirmations[token] = data
		h.confirmationMu.Unlock()

		return token
	}
}

var (
	maxConfirmationCode = big.NewInt(1000000)
	big1                = big.NewInt(1)
)

func generateConfirmationCode() string {
	n, err := rand.Int(rand.Reader, maxConfirmationCode)
	if err != nil {
		panic(err)
	}

	n = n.Sub(n, big1)
	return fmt.Sprintf("%06d", n)
}
