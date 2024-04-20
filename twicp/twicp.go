package twicp

import (
	"context"

	"github.com/twipi/twipi/proto/out/twicppb"
)

// OptionController is an interface that describes a controller for options.
type OptionController interface {
	// Schema returns the schema describing the options.
	Schema(context.Context) (*twicppb.Schema, error)
	// Values returns the current vluaes of the options.
	Values(context.Context) (*twicppb.OptionValues, error)
	// ApplyValues applies the values of the options.
	// Only the values given in the values list should be changed.
	ApplyValues(context.Context, *twicppb.ApplyRequest) (*twicppb.ApplyResponse, error)
}
