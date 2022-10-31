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

-- NEW VERSION --

CREATE TABLE accounts (
	user_number TEXT PRIMARY KEY,
	twilio_number TEXT NOT NULL,
	discord_token TEXT NOT NULL UNIQUE
);

-- NEW VERSION --

CREATE TABLE numbers_muted (
	user_number TEXT PRIMARY KEY REFERENCES accounts(user_number),
	muted INT NOT NULL DEFAULT 0
);
