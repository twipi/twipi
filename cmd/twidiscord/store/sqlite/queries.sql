-- name: ChannelToSerial :one
INSERT OR IGNORE
	INTO channel_serials (user_id, channel_id, serial)
	VALUES (
		?,
		?,
		(SELECT COALESCE(MAX(serial) + 1, 1) FROM channel_serials WHERE channel_serials.user_id = ?))
	RETURNING serial;

-- name: SerialToChannel :one
SELECT channel_id FROM channel_serials WHERE user_id = ? AND serial = ? LIMIT 1;
