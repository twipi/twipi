package slashparser

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twicmd"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

type resultBox[T any] struct {
	Value T
	Error error
}

func TestParser(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		lookup    *twicmd.ServiceLookup
		result    *twicmdproto.Command
		resultErr error
	}{
		{
			name:   "parse positional arguments",
			body:   "/discord send DiscordGophers offtopic Hello, world!",
			lookup: mustLookupWithServices("./testdata/test_service.txtpb"),
			result: &twicmdproto.Command{
				Service: "discord",
				Command: "send",
				Arguments: []*twicmdproto.CommandArgument{
					{Name: "guild", Value: "DiscordGophers"},
					{Name: "channel", Value: "offtopic"},
					{Name: "message", Value: "Hello, world!"},
				},
			},
		},
		{
			name:   "parse positional arguments with spaces in trailing",
			body:   "/discord send DiscordGophers offtopic Hello,\n\n world!  ",
			lookup: mustLookupWithServices("./testdata/test_service.txtpb"),
			result: &twicmdproto.Command{
				Service: "discord",
				Command: "send",
				Arguments: []*twicmdproto.CommandArgument{
					{Name: "guild", Value: "DiscordGophers"},
					{Name: "channel", Value: "offtopic"},
					{Name: "message", Value: "Hello,\n\n world!"},
				},
			},
		},
		{
			name:   "parse named arguments",
			body:   `/discord send guild=DiscordGophers "chan"nel=offtopic message="Hello,` + "\n\n" + `world!"`,
			lookup: mustLookupWithServices("./testdata/test_service_named.txtpb"),
			result: &twicmdproto.Command{
				Service: "discord",
				Command: "send",
				Arguments: []*twicmdproto.CommandArgument{
					{Name: "guild", Value: "DiscordGophers"},
					{Name: "channel", Value: "offtopic"},
					{Name: "message", Value: "Hello,\n\nworld!"},
				},
			},
		},
	}

	ctx := context.Background()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &twismsproto.MessageBody{
				Text: &twismsproto.TextBody{Text: test.body},
			}

			command, err := NewParser().Parse(ctx, test.lookup, body)
			assert.Equal(t, test.resultErr, err, "parse command error")

			if diff := cmp.Diff(test.result, command, protocmp.Transform()); diff != "" {
				t.Errorf("unexpected command (-want +got):\n%s", diff)
			}
		})
	}
}

/*
func FuzzParser_Positional(f *testing.F) {
	ctx := context.Background()
	parser := mustParserWithServices("./testdata/test_service.txtpb")

	f.Add("/discord send DiscordGophers offtopic Hello, world!")
	f.Add("/discord send DiscordGophers offtopic Hello,\n\n world!  ")

	// Fuzz to prevent panics.
	f.Fuzz(func(t *testing.T, body string) {
		parser.Parse(ctx, &twismsproto.MessageBody{
			Text: &twismsproto.TextBody{Text: body},
		})
	})
}

func FuzzParser_Named(f *testing.F) {
	ctx := context.Background()
	parser := mustParserWithServices("./testdata/test_service.txtpb")

	f.Add("/discord send guild=DiscordGophers channel=offtopic message=\"Hello, world!\"")
	f.Add("/discord send guild=DiscordGophers \"chan\"nel=offtopic message=\"Hello,\n\nworld!\"")

	// Fuzz to prevent panics.
	f.Fuzz(func(t *testing.T, body string) {
		bodyProto := &twismsproto.MessageBody{
			Text: &twismsproto.TextBody{Text: body},
		}

		cmd1, err1 := parser.Parse(ctx, bodyProto)
		cmd2, err2 := parser.Parse(ctx, bodyProto)

		r1 := resultBox[*twicmdproto.Command]{cmd1, err1}
		r2 := resultBox[*twicmdproto.Command]{cmd2, err2}
		if !cmp.Equal(r1, r2, protocmp.Transform()) {
			t.Errorf("commands are not equal:\n%s", cmp.Diff(cmd1, cmd2, protocmp.Transform()))
		}
	})
}
*/

type testService struct {
	name    string
	service *twicmdproto.Service
}

func (t *testService) Name() string { return t.name }

func (t *testService) Service(ctx context.Context) (*twicmdproto.Service, error) {
	return t.service, nil
}

func (t *testService) Execute(context.Context, *twicmdproto.Command) (*twismsproto.MessageBody, error) {
	panic("not implemented")
}

func mustLookupWithServices(serviceFiles ...string) *twicmd.ServiceLookup {
	lookup := twicmd.NewCommandLookup()
	for _, file := range serviceFiles {
		service := mustReadPrototext[*twicmdproto.Service](file)
		lookup.Register(&testService{service.Name, service})
	}
	return lookup
}

func mustReadPrototext[T proto.Message](path string) T {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintln("failed to read file:", err))
	}

	v := reflect.New(reflect.TypeFor[T]().Elem()).Interface().(T)
	if err := prototext.Unmarshal(b, v); err != nil {
		panic(fmt.Sprintln("failed to unmarshal prototext:", err))
	}

	return v
}
