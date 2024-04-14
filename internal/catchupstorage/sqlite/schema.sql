CREATE TABLE messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	to_number TEXT NOT NULL,
	created_at INTEGER NOT NULL DEFAULT CURRENT_TIMESTAMP,
	protobuf_data BLOB NOT NULL
);

CREATE INDEX messages_to_number_created_at_idx ON messages(id, to_number, created_at);
