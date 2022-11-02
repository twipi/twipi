package module

import (
	"context"
	"strings"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/ningen/v3"
	"github.com/diamondburned/ningen/v3/discordmd"
	"github.com/diamondburned/twikit/logger"
	"github.com/yuin/goldmark/ast"
)

func renderText(ctx context.Context,
	state *ningen.State, body string, srcMessage *discord.Message) string {

	src := []byte(body)
	n := discordmd.ParseWithMessage(src, *state.Cabinet, srcMessage, false)
	var s strings.Builder

	var walk ast.Walker
	walk = func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		switch n := n.(type) {
		case *ast.Blockquote:
			if enter {
				// A blockquote contains a paragraph each line. Because Discord.
				for child := n.FirstChild(); child != nil; child = child.NextSibling() {
					s.WriteString("> ")
					walk(child, true)
				}
			}
			// We've already walked over children ourselves.
			return ast.WalkSkipChildren, nil

		case *ast.Paragraph:
			if !enter {
				s.WriteByte('\n')
			}
		case *ast.FencedCodeBlock:
			if enter {
				// Write the body
				s.WriteByte('\n')
				for i := 0; i < n.Lines().Len(); i++ {
					line := n.Lines().At(i)
					s.WriteString("$ " + string(line.Value(src)))
				}
				s.WriteByte('\n')
			}
		case *ast.Link:
			if enter {
				s.WriteString(string(n.Title) + " (" + string(n.Destination) + ")")
			}
		case *ast.AutoLink:
			if enter {
				s.Write(n.URL(src))
			}
		case *discordmd.Emoji:
			if enter {
				s.WriteString(":" + string(n.Name) + ":")
			}
		case *discordmd.Mention:
			if enter {
				switch {
				case n.Channel != nil:
					s.WriteString("#" + n.Channel.Name)
				case n.GuildUser != nil:
					s.WriteString("@" + n.GuildUser.Username)
				case n.GuildRole != nil:
					s.WriteString("@" + n.GuildRole.Name)
				}
			}
		case *ast.String:
			if enter {
				s.Write(n.Value)
			}
		case *ast.Text:
			if enter {
				s.Write(n.Segment.Value(src))
				switch {
				case n.HardLineBreak():
					s.WriteByte('\n')
					s.WriteByte('\n')
				case n.SoftLineBreak():
					s.WriteByte('\n')
				}
			}
		}

		return ast.WalkContinue, nil
	}

	if err := ast.Walk(n, walk); err != nil {
		log := logger.FromContext(ctx, "markdown")
		log.Println("cannot walk:", err)
		return string(src)
	}

	return s.String()
}
