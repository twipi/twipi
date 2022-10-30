PRAGMA strict = ON;
PRAGMA journal_mode = WAL;

-- NEW VERSION --

CREATE TABLE channel_serials (
	user_id BIGINT NOT NULL,
	channel_id BIGINT NOT NULL,
	serial INTEGER NOT NULL,
	UNIQUE(user_id, channel_id),
	UNIQUE(user_id, serial)
);
