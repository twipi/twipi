-- name: MessagesAfter :many
SELECT * FROM messages WHERE
	id > ? AND
	created_at >= ?
	AND (from_number IN (sqlc.slice('from_numbers')) OR to_number IN (sqlc.slice('to_numbers')))
ORDER BY id ASC
LIMIT 100;

-- name: InsertMessage :exec
INSERT INTO messages (from_number, to_number, created_at, protobuf_data) VALUES (?, ?, ?, ?);
