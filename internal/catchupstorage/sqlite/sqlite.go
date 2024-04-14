// Package sqlite implements a SQLite storage backend for the message queue.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "embed"

	"github.com/twipi/cfgutil"
	"github.com/twipi/twipi/internal/catchupstorage/sqlite/queries"
	"github.com/twipi/twipi/internal/xiter"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"google.golang.org/protobuf/proto"
	"libdb.so/lazymigrate"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

const pragma = `
	PRAGMA journal_mode=WAL;
	PRAGMA foreign_keys=ON;
	PRAGMA strict=ON;
`

// StorageConfig is the configuration for the "sqlite" storage backend.
type StorageConfig struct {
	// Path is the path to/URI for the SQLite database file.
	Path string `json:"sqlite_path"`
	// MaxAge is the maximum age of messages to keep in the database.
	MaxAge cfgutil.Duration `json:"max_age"`
}

// MessageStorage is the SQLite storage backend for the message queue.
type MessageStorage struct {
	db *sql.DB
	q  *queries.Queries
}

// NewMessageStorage creates a new SQLite storage backend for the message queue.
func NewMessageStorage(ctx context.Context, cfg *StorageConfig) (*MessageStorage, error) {
	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("could not open SQLite database: %w", err)
	}

	if _, err := db.ExecContext(ctx, pragma); err != nil {
		return nil, fmt.Errorf("could not set SQLite PRAGMA: %w", err)
	}

	if err := lazymigrate.Migrate(ctx, db, schema); err != nil {
		return nil, fmt.Errorf("could not migrate SQLite database: %w", err)
	}

	return &MessageStorage{
		db: db,
		q:  queries.New(db),
	}, nil
}

func (s *MessageStorage) Close() error {
	return s.db.Close()
}

func (s *MessageStorage) RetrieveMessages(ctx context.Context, since time.Time, toNumbers []string) xiter.Seq2[*twismsproto.Message, error] {
	return func(yield func(*twismsproto.Message, error) bool) bool {
		var nextID int64
		for ctx.Err() == nil {
			rows, err := s.q.MessagesAfter(ctx, queries.MessagesAfterParams{
				ID:        nextID,
				CreatedAt: since.Unix(),
				ToNumbers: toNumbers,
			})
			if err != nil {
				yield(nil, fmt.Errorf("could not query messages: %w", err))
				return false
			}

			if len(rows) == 0 {
				return true
			}

			for _, row := range rows {
				msg := &twismsproto.Message{}
				if err := proto.Unmarshal(row.ProtobufData, msg); err != nil {
					yield(nil, fmt.Errorf("could not unmarshal message: %w", err))
					return false
				}

				if !yield(msg, nil) {
					return false
				}
			}

			nextID = rows[len(rows)-1].ID
		}

		if ctx.Err() != nil {
			yield(nil, ctx.Err())
		}

		return false
	}
}

func (s *MessageStorage) StoreMessage(ctx context.Context, msg *twismsproto.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("could not marshal message: %w", err)
	}

	if err := s.q.InsertMessage(ctx, queries.InsertMessageParams{
		ToNumber:     msg.To,
		CreatedAt:    msg.Timestamp.AsTime().Unix(),
		ProtobufData: data,
	}); err != nil {
		return fmt.Errorf("could not insert message: %w", err)
	}

	return nil
}
