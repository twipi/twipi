package slashparser

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/twipi/twipi/proto/out/twicmdproto"
	"github.com/twipi/twipi/proto/out/twismsproto"
	"github.com/twipi/twipi/twicmd"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

var shellParser = syntax.NewParser(
	syntax.Variant(syntax.LangPOSIX),
)

// Parser is a command parser that parses slash commands.
type Parser struct {
	services *xsync.MapOf[string, serviceLookup]
}

type serviceLookup struct {
	service  *twicmdproto.Service
	commands *xsync.MapOf[string, *twicmdproto.CommandDescription]
}

// NewParser creates a new Parser.
func NewParser() *Parser {
	return &Parser{
		services: xsync.NewMapOf[string, serviceLookup](),
	}
}

// RegisterService registers a service for the command parser.
func (p *Parser) RegisterService(ctx context.Context, service *twicmdproto.Service) error {
	if err := twicmd.ValidateService(service); err != nil {
		return err
	}

	lookup := serviceLookup{
		service: service,
		commands: xsync.NewMapOfPresized[string, *twicmdproto.CommandDescription](
			len(service.Commands)),
	}
	for _, cmd := range service.Commands {
		lookup.commands.Store(cmd.Name, cmd)
	}
	p.services.Store(service.Name, lookup)
	return nil
}

// Parse parses the given message body and returns the parsed command.
func (p *Parser) Parse(ctx context.Context, body *twismsproto.MessageBody) (*twicmdproto.Command, error) {
	if body.Text == nil {
		return nil, fmt.Errorf("empty message body")
	}

	startingWords, rest, err := p.parseNWords(body.Text.Text, 2)
	if err != nil {
		return nil, fmt.Errorf("failed to parse command start: %w", err)
	}

	if !strings.HasPrefix(startingWords[0], "/") {
		return nil, fmt.Errorf("parsing non-slash command %q", body.Text.Text)
	}

	serviceName := startingWords[0][1:]
	commandName := startingWords[1]

	lookup, ok := p.services.Load(serviceName)
	if !ok {
		return nil, fmt.Errorf("unknown service %q", serviceName)
	}

	service := lookup.service
	command, ok := lookup.commands.Load(commandName)
	if !ok {
		return nil, fmt.Errorf("unknown command %q for service %q", commandName, serviceName)
	}

	arguments, err := p.parseCommand(service, command, rest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse command %q: %w", command.Name, err)
	}

	return &twicmdproto.Command{
		Service:   service.Name,
		Command:   command.Name,
		Arguments: arguments,
	}, nil
}

func (p *Parser) parseCommand(
	service *twicmdproto.Service,
	command *twicmdproto.CommandDescription,
	args string,
) ([]*twicmdproto.CommandArgument, error) {
	arguments := make([]*twicmdproto.CommandArgument, 0, len(command.Arguments))
	argumentMap := make(map[string]string, len(command.Arguments))

	appendArgument := func(name, value string) {
		argumentMap[name] = value
		arguments = append(arguments, &twicmdproto.CommandArgument{
			Name:  name,
			Value: value,
		})
	}

	// If the command has positional arguments, we expect them to be present.
	// This avoids confusing cases where we have to automatically detect it.
	if len(command.ArgumentPositions) > 0 {
		// Positional arguments are required.
		positionalArgs := command.ArgumentPositions
		if command.ArgumentTrailing {
			// If trailing, then trim the last argument. We'll parse this
			// differently.
			positionalArgs = positionalArgs[:len(positionalArgs)-1]
		}

		words, rest, err := p.parseNWords(args, len(positionalArgs))
		if err != nil {
			return nil, fmt.Errorf("failed to split positional arguments: %w", err)
		}

		for i, name := range positionalArgs {
			arg := command.Arguments[name]

			if err := assertHintedValue(words[i], arg.Hint); err != nil {
				return nil, fmt.Errorf("invalid value %q for argument %q: %w", args[0], name, err)
			}

			appendArgument(name, words[i])
		}

		if command.ArgumentTrailing {
			name := command.ArgumentPositions[len(command.ArgumentPositions)-1]
			arg := command.Arguments[name]

			if err := assertHintedValue(rest, arg.Hint); err != nil {
				return nil, fmt.Errorf("invalid value %q for argument %q: %w", rest, name, err)
			}

			appendArgument(name, rest)
		}
	} else {
		words, _, err := p.parseNWords(args, -1)
		if err != nil {
			return nil, fmt.Errorf("failed to split named arguments: %w", err)
		}

		// Parse all arguments as named arguments.
		// Parse args into our arguments map.
		for _, word := range words {
			k, v, ok := strings.Cut(word, "=")
			if !ok {
				return nil, fmt.Errorf("invalid named argument %q, expected x=y syntax", word)
			}

			if _, ok := argumentMap[k]; ok {
				return nil, fmt.Errorf("duplicate argument %q", k)
			}

			arg, ok := command.Arguments[k]
			if !ok {
				return nil, fmt.Errorf("unknown argument %q for command %q", k, command.Name)
			}

			if err := assertHintedValue(v, arg.Hint); err != nil {
				return nil, fmt.Errorf("invalid value %q for argument %q: %w", v, k, err)
			}

			appendArgument(k, v)
		}
	}

	// Verify that all required arguments are present.
	for name, argDesc := range command.Arguments {
		if argDesc.Required && argumentMap[name] == "" {
			return nil, fmt.Errorf("missing required argument %q", name)
		}
	}

	return arguments, nil
}

// parseNWords parses n words from the given string. It returns the parsed words,
// the remaining string, and any error encountered. If n=-1, it will parse all words
// in the string.
func (p *Parser) parseNWords(s string, n int) ([]string, string, error) {
	s = strings.TrimSpace(s)
	if n == 0 {
		return nil, s, nil
	}

	log.Printf("parseNWords: %q, %d", s, n)

	words := make([]*syntax.Word, 0, max(n, 3))
	err := shellParser.Words(strings.NewReader(s), func(word *syntax.Word) bool {
		words = append(words, word)
		return n < 0 || len(words) < n
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to split words: %w", err)
	}
	if n > 0 && len(words) < n {
		return nil, "", fmt.Errorf("expected %d words, got %d", n, len(words))
	}

	lits := make([]string, len(words))
	for i, word := range words {
		lit, err := shLiteral(word)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse word: %w", err)
		}
		lits[i] = lit
	}

	last := words[len(words)-1]
	s = strings.TrimSpace(s[last.End().Offset():])

	return lits, s, nil
}

// shLiteral returns the literal string representation of the given shell word.
func shLiteral(word *syntax.Word) (string, error) {
	return expand.Literal(nil, word)
}

func assertHintedValue(value string, hint twicmdproto.CommandArgumentHint) error {
	switch hint {
	case twicmdproto.CommandArgumentHint_COMMAND_ARGUMENT_HINT_INTEGER:
		_, err := strconv.Atoi(value)
		return err
	case twicmdproto.CommandArgumentHint_COMMAND_ARGUMENT_HINT_NUMBER:
		_, err := strconv.ParseFloat(value, 64)
		return err
	}
	return nil
}
