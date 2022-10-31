package routes

import (
	"net/http"

	"github.com/diamondburned/twikit/cmd/twid/twidiscord"
	"github.com/diamondburned/twikit/cmd/twid/twidiscord/web"
	"github.com/pkg/errors"
)

var confirmTmpl = web.Templates.Register("confirm", "routes/confirm.html")
var successTmpl = web.Templates.Register("success", "routes/success.html")

func (h *registerHandler) confirmGET(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(registerTokenCookie)
	if cookie == nil {
		// No cookie? Redirect to the register page.
		http.Redirect(w, r, "/discord/register", http.StatusTemporaryRedirect)
		return
	}

	// Validate our confirmation token in the cookie.
	h.confirmationMu.Lock()
	_, ok := h.confirmations[cookie.Value]
	h.confirmationMu.Unlock()

	if !ok {
		// No confirmation? Redirect to the register page.
		http.Redirect(w, r, "/discord/register", http.StatusTemporaryRedirect)
		return
	}

	confirmTmpl.Execute(w, nil)
}

func (h *registerHandler) confirmPOST(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(registerTokenCookie)
	if cookie == nil {
		// No cookie? Redirect to the register page.
		http.Redirect(w, r, "/discord/register", http.StatusTemporaryRedirect)
		return
	}

	// Validate our confirmation token in the cookie.
	h.confirmationMu.Lock()
	registerData, ok := h.confirmations[cookie.Value]
	h.confirmationMu.Unlock()

	if !ok {
		// No confirmation? Redirect to the register page.
		http.Redirect(w, r, "/discord/register", http.StatusTemporaryRedirect)
		return
	}

	if err := r.ParseForm(); err != nil {
		renderError(w, http.StatusBadRequest, err)
		return
	}

	confirmationCode := r.FormValue("confirmation-code")
	if confirmationCode != registerData.ConfirmationCode {
		// Invalid confirmation code.
		renderError(w, http.StatusBadRequest, errors.New("wrong confirmation code"))
		return
	}

	// Add the user to the database.
	if err := h.accountAdder.AddAccount(r.Context(), twidiscord.Account{
		UserNumber:   registerData.UserNumber,
		TwilioNumber: registerData.TwilioNumber,
		DiscordToken: registerData.DiscordToken,
	}); err != nil {
		renderError(w, http.StatusInternalServerError, errors.Wrap(err, "cannot add your account"))
		return
	}

	// Delete our confirmation.
	h.confirmationMu.Lock()
	delete(h.confirmations, cookie.Value)
	h.confirmationMu.Unlock()

	// Invalidate the cookie.
	cookie.Value = ""
	cookie.MaxAge = -1
	http.SetCookie(w, cookie)

	// Print a short OK message.
	successTmpl.Execute(w, nil)
}
