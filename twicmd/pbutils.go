package twicmd

import "github.com/twipi/twipi/proto/out/twicmdproto"

// MapArguments maps the given list of command arguments to a map of key-value
// pairs.
func MapArguments(arguments []*twicmdproto.CommandArgument) map[string]string {
	m := make(map[string]string, len(arguments))
	for _, arg := range arguments {
		m[arg.Name] = arg.Value
	}
	return m
}
