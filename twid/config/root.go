package config

import (
	"bytes"
	"encoding/json"
)

// Root is the root configuration for the twid package.
type Root struct {
	ListenAddr string `json:"listen_addr"`
	Twisms     Twisms `json:"twisms"`
}

// Twisms is the configuration for package Twisms.
type Twisms struct {
	Services []TwismsService `json:"services"`
}

// TwismsService is the configuration for a Twisms service.
// The user must specify the module name and the configuration for that module
// in the same JSON object.
type TwismsService struct {
	Module string `json:"module"`

	raw json.RawMessage
}

// UnmarshalJSON implements [json.Unmarshaler].
func (t *TwismsService) UnmarshalJSON(b []byte) error {
	type raw TwismsService
	if err := json.Unmarshal(b, (*raw)(t)); err != nil {
		return err
	}
	*t = TwismsService(*t)
	t.raw = json.RawMessage(bytes.Clone(b))
	return nil
}

// MarshalJSON implements [json.Marshaler]. It never fails.
func (t *TwismsService) MarshalJSON() ([]byte, error) {
	return t.raw, nil
}
