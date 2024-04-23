package main

import (
	"fmt"
	"reflect"

	"github.com/twipi/twipi/proto/out/twicmdproto"
)

func main() {
	t := reflect.TypeFor[*twicmdproto.ExecuteRequest]()
	fmt.Printf("%v\n", t.Kind() == reflect.Ptr)
}
