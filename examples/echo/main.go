package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/darui3018823/dgo"
)

type optionMap = map[string]*dgo.ApplicationCommandInteractionDataOption

func parseOptions(options []*dgo.ApplicationCommandInteractionDataOption) (om optionMap) {
	om = make(optionMap)
	for _, opt := range options {
		om[opt.Name] = opt
	}
	return
}

func interactionAuthor(i *dgo.Interaction) *dgo.User {
	if i.Member != nil {
		return i.Member.User
	}
	return i.User
}

func handleEcho(s *dgo.Session, i *dgo.InteractionCreate, opts optionMap) {
	builder := new(strings.Builder)
	if v, ok := opts["author"]; ok && v.BoolValue() {
		author := interactionAuthor(i.Interaction)
		builder.WriteString("**" + author.String() + "** says: ")
	}
	builder.WriteString(opts["message"].StringValue())

	err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
		Type: dgo.InteractionResponseChannelMessageWithSource,
		Data: &dgo.InteractionResponseData{
			Content: builder.String(),
		},
	})

	if err != nil {
		log.Panicf("could not respond to interaction: %s", err)
	}
}

var commands = []*dgo.ApplicationCommand{
	{
		Name:        "echo",
		Description: "Say something through a bot",
		Options: []*dgo.ApplicationCommandOption{
			{
				Name:        "message",
				Description: "Contents of the message",
				Type:        dgo.ApplicationCommandOptionString,
				Required:    true,
			},
			{
				Name:        "author",
				Description: "Whether to prepend message's author",
				Type:        dgo.ApplicationCommandOptionBoolean,
			},
		},
	},
}

var (
	Token = flag.String("token", "", "Bot authentication token")
	App   = flag.String("app", "", "Application ID")
	Guild = flag.String("guild", "", "Guild ID")
)

func main() {
	flag.Parse()
	if *App == "" {
		log.Fatal("application id is not set")
	}

	session, _ := dgo.New("Bot " + *Token)

	session.AddHandler(func(s *dgo.Session, i *dgo.InteractionCreate) {
		if i.Type != dgo.InteractionApplicationCommand {
			return
		}

		data := i.ApplicationCommandData()
		if data.Name != "echo" {
			return
		}

		handleEcho(s, i, parseOptions(data.Options))
	})

	session.AddHandler(func(s *dgo.Session, r *dgo.Ready) {
		log.Printf("Logged in as %s", r.User.String())
	})

	_, err := session.ApplicationCommandBulkOverwrite(*App, *Guild, commands)
	if err != nil {
		log.Fatalf("could not register commands: %s", err)
	}

	err = session.Open()
	if err != nil {
		log.Fatalf("could not open session: %s", err)
	}

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt)
	<-sigch

	err = session.Close()
	if err != nil {
		log.Printf("could not close session gracefully: %s", err)
	}
}
