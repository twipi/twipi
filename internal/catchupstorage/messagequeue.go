// Package catchupstorage provides a persistent message queue for Twisms.
// It allows clients to catch up on messages that were sent while they were
// offline. Since this is transport-dependent (some SMS services come with their
// own message queue), it's only used internally.
package catchupstorage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/twipi/twipi/internal/catchupstorage/sqlite"
	"github.com/twipi/twipi/internal/xiter"
	"github.com/twipi/twipi/proto/out/twismsproto"
)

// MessageQueueConfig is the configuration for a MessageQueue.
type MessageQueueConfig struct {
	// SQLite is the configuration for the SQLite storage backend.
	SQLite *sqlite.StorageConfig `json:"sqlite"`
}

type messageStorer interface {
	io.Closer
	// RetrieveMessages retrieves messages from the message queue.
	RetrieveMessages(ctx context.Context, since time.Time, toNumbers []string) xiter.Seq2[*twismsproto.Message, error]
	// StoreMessage stores the message into the message queue.
	StoreMessage(ctx context.Context, msg *twismsproto.Message) error
}

// MessageQueue implements a persistent message queue for Twisms.
type MessageQueue struct {
	storer messageStorer
	logger *slog.Logger
}

// NewMessageQueue creates a new MessageQueue.
func NewMessageQueue(ctx context.Context, cfg *MessageQueueConfig, logger *slog.Logger) (*MessageQueue, error) {
	var storer messageStorer
	var err error

	switch {
	case cfg.SQLite != nil:
		logger = logger.With(
			"message_storage", "sqlite",
			"message_storage.sqlite_path", cfg.SQLite.Path)

		storer, err = sqlite.NewMessageStorage(ctx, cfg.SQLite, logger)
		if err != nil {
			return nil, fmt.Errorf("could not create SQLite message storage: %w", err)
		}

		logger.Info("created SQLite message storage")

	default:
		return nil, fmt.Errorf("no storage backend configured")
	}

	return &MessageQueue{
		storer: storer,
		logger: logger,
	}, nil
}

func (mq *MessageQueue) Close() error {
	if err := mq.storer.Close(); err != nil {
		mq.logger.Warn(
			"could not close message storage",
			"err", err)
		return err
	}
	mq.logger.Debug("closed message storage")
	return nil
}

func (mq *MessageQueue) RetrieveMessages(ctx context.Context, since time.Time, numbers []string) xiter.Seq2[*twismsproto.Message, error] {
	return mq.storer.RetrieveMessages(ctx, since, numbers)
}

func (mq *MessageQueue) StoreMessage(ctx context.Context, msg *twismsproto.Message) error {
	if err := mq.storer.StoreMessage(ctx, msg); err != nil {
		mq.logger.Error(
			"could not store message",
			"err", err)
		return err
	}
	return nil
}
