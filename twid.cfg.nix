{
	port,
	discordPort,
	phoneNumber,
	stateDirectory,
}:

{
	listen_addr = "localhost:${port}";
	twisms = {
		services = [
			{
				module = "wsbridge_server";
				# HTTP path: localhost:8080/sms/ws
				http_path = "/sms/ws";
				# The phone number that the WS accepts.
				# This is a list of phone numbers that are allowed to send messages using
				# the WS server.
				phone_numbers = [ phoneNumber ];
				# Time to wait for an acknowledgement from the client.
				acknowledgement_timeout = "5s";
				# Set up the persistent message queue using SQLite.
				message_queue = {
					sqlite = {
						path = "${stateDirectory}/wsbridge-queue.sqlite3";
						max_age = "1400h";
					};
				};
			}
		];
	};
	twicmd = {
		parsers = [
			{
				module = "slash";
			}
		];
		services = [
			{
				module = "http";
				name = "discord";
				base_url = "http://localhost:${discordPort}";
				control_panel = {
					module = "http";
					base_url = "http://localhost:${discordPort}/cp";
				};
			}
		];
	};
}
