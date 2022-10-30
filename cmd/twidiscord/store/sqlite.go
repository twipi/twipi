package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/twikit/cmd/twidiscord/store/sqlite"
	"github.com/pkg/errors"

	_ "embed"

	_ "modernc.org/sqlite"
)

//go:embed sqlite/schema.sql
var sqliteSchema string

// SQLite is a SQLite database.
type SQLite struct {
	q  *sqlite.Queries
	db *sql.DB
}

// OpenSQLite creates a new SQLite database.
func OpenSQLite(ctx context.Context, path string, ro bool) (*SQLite, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open SQLite database")
	}

	if ro {
		_, err = sqlDB.ExecContext(ctx, `PRAGMA query_only = yes;`)
		if err != nil {
			return nil, errors.Wrap(err, "failed to set query_only")
		}
	} else {
		var userVersion int
		if err := sqlDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
			return nil, errors.Wrap(err, "failed to get user_version")
		}

		versions := strings.Split(sqliteSchema, "-- NEW VERSION --\n")
		for i := userVersion; i < len(versions); i++ {
			_, err := sqlDB.ExecContext(ctx, versions[i])
			if err != nil {
				return nil, errors.Wrapf(err, "cannot apply migration %d", i)
			}
		}

		if _, err := sqlDB.ExecContext(ctx,
			fmt.Sprintf(
				"PRAGMA user_version = %d",
				len(versions),
			),
		); err != nil {
			return nil, errors.Wrap(err, "failed to set user_version")
		}
	}

	return &SQLite{
		q:  sqlite.New(sqlDB),
		db: sqlDB,
	}, nil
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

func (s *SQLite) ChannelToSerial(ctx context.Context, uID discord.UserID, chID discord.ChannelID) (int, error) {
	n, err := s.q.ChannelToSerial(ctx, sqlite.ChannelToSerialParams{
		UserID:    int64(uID),
		ChannelID: int64(chID),
		UserID_2:  int64(uID),
	})
	return int(n), sqliteErr(err)
}

func (s *SQLite) SerialToChannel(ctx context.Context, uID discord.UserID, serial int) (discord.ChannelID, error) {
	id, err := s.q.SerialToChannel(ctx, sqlite.SerialToChannelParams{
		UserID: int64(uID),
		Serial: int64(serial),
	})
	return discord.ChannelID(id), sqliteErr(err)
}

func sqliteErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
