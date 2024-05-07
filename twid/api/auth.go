package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	mathrand "math/rand/v2"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/twipi/twipi/proto/out/twidpb"
	"github.com/twipi/twipi/twisms"
	"google.golang.org/protobuf/types/known/timestamppb"
	"libdb.so/ctxt"
	"libdb.so/hrt"
)

const (
	loginCodeExpiration = 5 * time.Minute
	sessionExpiration   = 5 * 24 * time.Hour
)

var errInvalidLogin = hrt.WrapHTTPError(401, fmt.Errorf("invalid login or session"))

type authHandler struct {
	sms      twisms.MessageSender
	logger   *slog.Logger
	codes    *xsync.MapOf[loginCode, authSession]
	sessions *xsync.MapOf[string, authSession]
}

func newAuthHandler(sms twisms.MessageSender, logger *slog.Logger) *authHandler {
	return &authHandler{
		sms:      sms,
		logger:   logger,
		codes:    xsync.NewMapOf[loginCode, authSession](),
		sessions: xsync.NewMapOf[string, authSession](),
	}
}

func (h *authHandler) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if !strings.HasPrefix(token, "Bearer ") {
			writeError(w, errInvalidLogin)
			return
		}
		token = strings.TrimPrefix(token, "Bearer ")

		session, ok := h.sessions.Load(token)
		if !ok {
			writeError(w, errInvalidLogin)
			return
		}

		if session.Expired() {
			h.sessions.Delete(token)

			writeError(w, errInvalidLogin)
			return
		}

		session.ExpiresAt = time.Now().Add(sessionExpiration).Unix()
		h.sessions.Store(token, session)

		ctx := ctxt.With(r.Context(), session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

const verificationMessage_ = `
	Your Twipi verification code is %s.
	This code expires in 5 minutes.
	If you didn't request this code, please ignore this message.
`

var verificationMessage = strings.TrimSpace(strings.NewReplacer(
	"\t", "",
	"\r", "",
).Replace(verificationMessage_))

func (h *authHandler) loginPhase1(ctx context.Context, req *twidpb.LoginPhase1Request) (hrt.None, error) {
	code, err := generateLoginCode(h.codes, authSession{
		PhoneNumber: req.PhoneNumber,
		ExpiresAt:   time.Now().Add(loginCodeExpiration).Unix(),
	})
	if err != nil {
		h.logger.Error(
			"failed to generate random auth code",
			"err", err)
		return hrt.Empty, errInternal
	}

	h.logger.Debug(
		"phase 1: generated auth code",
		"code", code,
		"phone_number", req.PhoneNumber)

	body := twisms.NewTextBody(fmt.Sprintf(verificationMessage, code))
	if err := twisms.SendAutoTextMessage(ctx, h.sms, req.PhoneNumber, body); err != nil {
		h.codes.Delete(code)
		h.logger.Error(
			"failed to send verification code",
			"err", err)
		return hrt.Empty, errInternal
	}

	return hrt.Empty, nil
}

func (h *authHandler) loginPhase2(ctx context.Context, req *twidpb.LoginPhase2Request) (*twidpb.LoginResponse, error) {
	code, err := parseAuthCode(req.Code)
	if err != nil {
		return nil, hrt.WrapHTTPError(400, fmt.Errorf("invalid code: %w", err))
	}

	session, ok := h.codes.Load(code)
	if !ok || session.Expired() || session.PhoneNumber != req.PhoneNumber {
		return nil, errInvalidLogin
	}

	session = authSession{
		PhoneNumber: session.PhoneNumber,
		ExpiresAt:   time.Now().Add(sessionExpiration).Unix(),
	}

	token, err := generateAuthToken(h.sessions, session)
	if err != nil {
		h.logger.Error(
			"failed to generate auth token",
			"err", err)
		return nil, errInternal
	}

	h.logger.Debug(
		"phase 2: generated auth session",
		"code", code,
		"phone_number", session.PhoneNumber)

	return &twidpb.LoginResponse{
		Token:     token,
		ExpiresAt: timestamppb.New(time.Unix(session.ExpiresAt, 0)),
	}, nil
}

type loginCode int

func generateLoginCode[T any](m *xsync.MapOf[loginCode, T], v T) (loginCode, error) {
	var zero loginCode
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return zero, fmt.Errorf("failed to generate seed: %w", err)
	}
	prng := mathrand.New(mathrand.NewChaCha8(seed))

	for iter := 0; iter < 1_000; iter++ {
		// current vibe: 7 digits
		const minCode = 1_000_000
		const maxCode = 9_999_999
		code := loginCode(prng.IntN(maxCode-minCode) + minCode)

		_, exists := m.LoadOrStore(code, v)
		if exists {
			continue
		}

		return code, nil
	}

	return zero, fmt.Errorf("timed out generating unique code")
}

func parseAuthCode(s string) (loginCode, error) {
	i, err := strconv.Atoi(s)
	return loginCode(i), err
}

func (a loginCode) String() string {
	return strconv.Itoa(int(a))
}

type authSession struct {
	PhoneNumber string
	ExpiresAt   int64
}

func (t authSession) Expired() bool {
	return t.ExpiresAt < time.Now().Unix()
}

func generateAuthToken[T any](m *xsync.MapOf[string, T], v T) (string, error) {
	for iter := 0; iter < 1_000; iter++ {
		var r [24]byte
		if _, err := rand.Read(r[:]); err != nil {
			return "", err
		}
		token := base64.URLEncoding.EncodeToString(r[:])

		_, exists := m.LoadOrStore(token, v)
		if exists {
			continue
		}

		return token, nil
	}

	return "", fmt.Errorf("timed out generating unique token")
}
