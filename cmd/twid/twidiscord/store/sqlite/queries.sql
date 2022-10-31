-- name: NewChannelSerial :exec
INSERT OR IGNORE
	INTO channel_serials (user_id, channel_id, serial)
	VALUES (
		?,
		?,
		(SELECT COALESCE(MAX(serial) + 1, 1)
			FROM channel_serials
			WHERE channel_serials.user_id = ?)
	);

-- name: ChannelToSerial :one
SELECT serial FROM channel_serials WHERE user_id = ? AND channel_id = ?;

-- name: SerialToChannel :one
SELECT channel_id FROM channel_serials WHERE user_id = ? AND serial = ? LIMIT 1;

-- name: SetAccount :exec
REPLACE INTO accounts (user_number, twilio_number, discord_token) VALUES (?, ?, ?);

-- name: Account :one
SELECT twilio_number, discord_token FROM accounts WHERE user_number = ? LIMIT 1;

-- name: Accounts :many
SELECT user_number, twilio_number, discord_token FROM accounts;

-- name: NumberIsMuted :one
SELECT muted FROM numbers_muted WHERE user_number = ? LIMIT 1;

-- name: SetNumberMuted :exec
REPLACE INTO numbers_muted (user_number, muted) VALUES (?, ?);
