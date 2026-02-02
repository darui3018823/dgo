package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/darui3018823/dgo"
)

// Bot parameters
var (
	GuildID  = flag.String("guild", "", "Test guild ID")
	BotToken = flag.String("token", "", "Bot access token")
	AppID    = flag.String("app", "", "Application ID")
	Cleanup  = flag.Bool("cleanup", true, "Cleanup of commands")
)

var s *dgo.Session

func init() { flag.Parse() }

func init() {
	var err error
	s, err = dgo.New("Bot " + *BotToken)
	if err != nil {
		log.Fatalf("Invalid bot parameters: %v", err)
	}
}

func searchLink(message, format, sep string) string {
	return fmt.Sprintf(format, strings.Join(
		strings.Split(
			message,
			" ",
		),
		sep,
	))
}

var (
	commands = []dgo.ApplicationCommand{
		{
			Name: "rickroll-em",
			Type: dgo.UserApplicationCommand,
		},
		{
			Name: "google-it",
			Type: dgo.MessageApplicationCommand,
		},
		{
			Name: "stackoverflow-it",
			Type: dgo.MessageApplicationCommand,
		},
		{
			Name: "godoc-it",
			Type: dgo.MessageApplicationCommand,
		},
		{
			Name: "discordjs-it",
			Type: dgo.MessageApplicationCommand,
		},
		{
			Name: "discordpy-it",
			Type: dgo.MessageApplicationCommand,
		},
	}
	commandsHandlers = map[string]func(s *dgo.Session, i *dgo.InteractionCreate){
		"rickroll-em": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: "Operation rickroll has begun",
					Flags:   dgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				panic(err)
			}

			ch, err := s.UserChannelCreate(
				i.ApplicationCommandData().TargetID,
			)
			if err != nil {
				_, err = s.FollowupMessageCreate(i.Interaction, true, &dgo.WebhookParams{
					Content: fmt.Sprintf("Mission failed. Cannot send a message to this user: %q", err.Error()),
					Flags:   dgo.MessageFlagsEphemeral,
				})
				if err != nil {
					panic(err)
				}
			}
			_, err = s.ChannelMessageSend(
				ch.ID,
				fmt.Sprintf("%s sent you this: https://youtu.be/dQw4w9WgXcQ", i.Member.Mention()),
			)
			if err != nil {
				panic(err)
			}
		},
		"google-it": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: searchLink(
						i.ApplicationCommandData().Resolved.Messages[i.ApplicationCommandData().TargetID].Content,
						"https://google.com/search?q=%s", "+"),
					Flags: dgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				panic(err)
			}
		},
		"stackoverflow-it": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: searchLink(
						i.ApplicationCommandData().Resolved.Messages[i.ApplicationCommandData().TargetID].Content,
						"https://stackoverflow.com/search?q=%s", "+"),
					Flags: dgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				panic(err)
			}
		},
		"godoc-it": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: searchLink(
						i.ApplicationCommandData().Resolved.Messages[i.ApplicationCommandData().TargetID].Content,
						"https://pkg.go.dev/search?q=%s", "+"),
					Flags: dgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				panic(err)
			}
		},
		"discordjs-it": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: searchLink(
						i.ApplicationCommandData().Resolved.Messages[i.ApplicationCommandData().TargetID].Content,
						"https://discord.js.org/#/docs/main/stable/search?query=%s", "+"),
					Flags: dgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				panic(err)
			}
		},
		"discordpy-it": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: searchLink(
						i.ApplicationCommandData().Resolved.Messages[i.ApplicationCommandData().TargetID].Content,
						"https://discordpy.readthedocs.io/en/stable/search.html?q=%s", "+"),
					Flags: dgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				panic(err)
			}
		},
	}
)

func main() {
	s.AddHandler(func(s *dgo.Session, r *dgo.Ready) {
		log.Println("Bot is up!")
	})

	s.AddHandler(func(s *dgo.Session, i *dgo.InteractionCreate) {
		if h, ok := commandsHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})

	cmdIDs := make(map[string]string, len(commands))

	for _, cmd := range commands {
		rcmd, err := s.ApplicationCommandCreate(*AppID, *GuildID, &cmd)
		if err != nil {
			log.Fatalf("Cannot create slash command %q: %v", cmd.Name, err)
		}

		cmdIDs[rcmd.ID] = rcmd.Name

	}

	err := s.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}
	defer s.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Graceful shutdown")

	if !*Cleanup {
		return
	}

	for id, name := range cmdIDs {
		err := s.ApplicationCommandDelete(*AppID, *GuildID, id)
		if err != nil {
			log.Fatalf("Cannot delete slash command %q: %v", name, err)
		}
	}

}
