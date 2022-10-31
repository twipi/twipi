package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/twikit/cmd/twidiscord/store/sqlite"
	"github.com/diamondburned/twikit/cmd/twidiscord/twidiscord"
	"github.com/diamondburned/twikit/twipi"
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

var _ twidiscord.Storer = (*SQLite)(nil)

// OpenSQLite creates a new SQLite database.
func OpenSQLite(ctx context.Context, uri string, ro bool) (*SQLite, error) {
	path := uri

	u, err := url.Parse(uri)
	if err == nil {
		path = u.Path
	}

	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, errors.Wrap(err, "failed to create SQLite directory")
	}

	sqlDB, err := sql.Open("sqlite", uri)
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
	})
	if err != nil {
		err = sqliteErr(err)
		if !errors.Is(err, ErrNotFound) {
			return 0, err
		}

		// If the channel is not found, then we need to create a new serial.
		err = s.q.NewChannelSerial(ctx, sqlite.NewChannelSerialParams{
			UserID:    int64(uID),
			ChannelID: int64(chID),
			UserID_2:  int64(uID),
		})
		if err != nil {
			return 0, sqliteErr(err)
		}

		n, err = s.q.ChannelToSerial(ctx, sqlite.ChannelToSerialParams{
			UserID:    int64(uID),
			ChannelID: int64(chID),
		})
	}

	return int(n), sqliteErr(err)
}

func (s *SQLite) SerialToChannel(ctx context.Context, uID discord.UserID, serial int) (discord.ChannelID, error) {
	id, err := s.q.SerialToChannel(ctx, sqlite.SerialToChannelParams{
		UserID: int64(uID),
		Serial: int64(serial),
	})
	return discord.ChannelID(id), sqliteErr(err)
}

func (s *SQLite) Account(ctx context.Context, userNumber twipi.PhoneNumber) (twidiscord.Account, error) {
	v, err := s.q.Account(ctx, string(userNumber))
	if err != nil {
		return twidiscord.Account{}, sqliteErr(err)
	}

	return twidiscord.Account{
		UserNumber:   userNumber,
		TwilioNumber: twipi.PhoneNumber(v.TwilioNumber),
		DiscordToken: v.DiscordToken,
	}, nil
}

func (s *SQLite) Accounts(ctx context.Context) ([]twidiscord.Account, error) {
	rows, err := s.q.Accounts(ctx)
	if err != nil {
		return nil, sqliteErr(err)
	}

	accs := make([]twidiscord.Account, len(rows))
	for i, v := range rows {
		accs[i] = twidiscord.Account{
			UserNumber:   twipi.PhoneNumber(v.UserNumber),
			TwilioNumber: twipi.PhoneNumber(v.TwilioNumber),
			DiscordToken: v.DiscordToken,
		}
	}

	return accs, nil
}

func (s *SQLite) SetAccount(ctx context.Context, info twidiscord.Account) error {
	err := s.q.SetAccount(ctx, sqlite.SetAccountParams{
		UserNumber:   string(info.UserNumber),
		TwilioNumber: string(info.TwilioNumber),
		DiscordToken: info.DiscordToken,
	})
	return sqliteErr(err)
}

func sqliteErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
