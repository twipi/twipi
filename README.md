# Twipi

Twipi is a unified SMS framework that allows easier integration of various web
services (e.g., calendar, chat, etc.) into SMS messaging.

For more information, refer to the [Twipi GitHub organization's
README](https://github.com/twipi).

## Does this have to do with Twilio?

Not anymore! Not at least until I decide to integrate it back in.

Twipi is currently a standalone project that does not rely on Twilio. Instead,
it designs its own tiny Protobuf protocol for SMS exchanges. This allows for
far more flexibility in the future.

For now, because registering a working phone number that allows texting using
Twilio requires formal verification, I have decided to temporarily remove
support for it.
