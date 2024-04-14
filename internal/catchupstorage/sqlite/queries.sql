-- name: MessagesAfter :many
SELECT * FROM messages WHERE
	id >= ? AND
	created_at >= ?
	AND to_number IN (sqlc.slice('to_numbers'))
LIMIT 100;

-- name: InsertMessage :exec
INSERT INTO messages (to_number, created_at, protobuf_data) VALUES (?, ?, ?);
