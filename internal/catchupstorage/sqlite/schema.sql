CREATE TABLE messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	from_number TEXT NOT NULL,
	to_number TEXT NOT NULL,
	created_at INTEGER NOT NULL DEFAULT CURRENT_TIMESTAMP,
	protobuf_data BLOB NOT NULL
);

CREATE INDEX messages_paginate_idx ON messages(id, from_number, to_number, created_at);
