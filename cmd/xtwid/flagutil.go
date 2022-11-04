package main

import "encoding/json"

// StringsFlag is a flag.Value that can be used to collect multiple values
// for a flag.
type StringsFlag []string

func (f *StringsFlag) String() string {
	b, err := json.Marshal(*f)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (f *StringsFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
