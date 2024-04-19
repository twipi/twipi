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

// StatusResponse creates a new [twicmdproto.ExecuteResponse] with the given status.
func StatusResponse(status string) *twicmdproto.ExecuteResponse {
	return &twicmdproto.ExecuteResponse{
		Response: &twicmdproto.ExecuteResponse_Status{
			Status: status,
		},
	}
}

// TextResponse creates a new [twicmdproto.ExecuteResponse] with the given text.
func TextResponse(text string) *twicmdproto.ExecuteResponse {
	return &twicmdproto.ExecuteResponse{
		Response: &twicmdproto.ExecuteResponse_Text{
			Text: text,
		},
	}
}
