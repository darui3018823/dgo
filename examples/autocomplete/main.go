package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/darui3018823/dgo"
)

// Bot parameters
var (
	GuildID        = flag.String("guild", "", "Test guild ID. If not passed - bot registers commands globally")
	BotToken       = flag.String("token", "", "Bot access token")
	RemoveCommands = flag.Bool("rmcmd", true, "Remove all commands after shutdowning or not")
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

var (
	commands = []*dgo.ApplicationCommand{
		{
			Name:        "single-autocomplete",
			Description: "Showcase of single autocomplete option",
			Type:        dgo.ChatApplicationCommand,
			Options: []*dgo.ApplicationCommandOption{
				{
					Name:         "autocomplete-option",
					Description:  "Autocomplete option",
					Type:         dgo.ApplicationCommandOptionString,
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "multi-autocomplete",
			Description: "Showcase of multiple autocomplete option",
			Type:        dgo.ChatApplicationCommand,
			Options: []*dgo.ApplicationCommandOption{
				{
					Name:         "autocomplete-option-1",
					Description:  "Autocomplete option 1",
					Type:         dgo.ApplicationCommandOptionString,
					Required:     true,
					Autocomplete: true,
				},
				{
					Name:         "autocomplete-option-2",
					Description:  "Autocomplete option 2",
					Type:         dgo.ApplicationCommandOptionString,
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	}

	commandHandlers = map[string]func(s *dgo.Session, i *dgo.InteractionCreate){
		"single-autocomplete": func(s *dgo.Session, i *dgo.InteractionCreate) {
			switch i.Type {
			case dgo.InteractionApplicationCommand:
				data := i.ApplicationCommandData()
				err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
					Type: dgo.InteractionResponseChannelMessageWithSource,
					Data: &dgo.InteractionResponseData{
						Content: fmt.Sprintf(
							"You picked %q autocompletion",
							// Autocompleted options do not affect usual flow of handling application command. They are ordinary options at this stage
							data.Options[0].StringValue(),
						),
					},
				})
				if err != nil {
					panic(err)
				}
			// Autocomplete options introduce a new interaction type (8) for returning custom autocomplete results.
			case dgo.InteractionApplicationCommandAutocomplete:
				data := i.ApplicationCommandData()
				choices := []*dgo.ApplicationCommandOptionChoice{
					{
						Name:  "Autocomplete",
						Value: "autocomplete",
					},
					{
						Name:  "Autocomplete is best!",
						Value: "autocomplete_is_best",
					},
					{
						Name:  "Choice 3",
						Value: "choice3",
					},
					{
						Name:  "Choice 4",
						Value: "choice4",
					},
					{
						Name:  "Choice 5",
						Value: "choice5",
					},
					// And so on, up to 25 choices
				}

				if data.Options[0].StringValue() != "" {
					choices = append(choices, &dgo.ApplicationCommandOptionChoice{
						Name:  data.Options[0].StringValue(), // To get user input you just get value of the autocomplete option.
						Value: "choice_custom",
					})
				}

				err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
					Type: dgo.InteractionApplicationCommandAutocompleteResult,
					Data: &dgo.InteractionResponseData{
						Choices: choices, // This is basically the whole purpose of autocomplete interaction - return custom options to the user.
					},
				})
				if err != nil {
					panic(err)
				}
			}
		},
		"multi-autocomplete": func(s *dgo.Session, i *dgo.InteractionCreate) {
			switch i.Type {
			case dgo.InteractionApplicationCommand:
				data := i.ApplicationCommandData()
				err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
					Type: dgo.InteractionResponseChannelMessageWithSource,
					Data: &dgo.InteractionResponseData{
						Content: fmt.Sprintf(
							"Option 1: %s\nOption 2: %s",
							data.Options[0].StringValue(),
							data.Options[1].StringValue(),
						),
					},
				})
				if err != nil {
					panic(err)
				}
			case dgo.InteractionApplicationCommandAutocomplete:
				data := i.ApplicationCommandData()
				var choices []*dgo.ApplicationCommandOptionChoice
				switch {
				// In this case there are multiple autocomplete options. The Focused field shows which option user is focused on.
				case data.Options[0].Focused:
					choices = []*dgo.ApplicationCommandOptionChoice{
						{
							Name:  "Autocomplete 4 first option",
							Value: "autocomplete_default",
						},
						{
							Name:  "Choice 3",
							Value: "choice3",
						},
						{
							Name:  "Choice 4",
							Value: "choice4",
						},
						{
							Name:  "Choice 5",
							Value: "choice5",
						},
					}
					if data.Options[0].StringValue() != "" {
						choices = append(choices, &dgo.ApplicationCommandOptionChoice{
							Name:  data.Options[0].StringValue(),
							Value: "choice_custom",
						})
					}

				case data.Options[1].Focused:
					choices = []*dgo.ApplicationCommandOptionChoice{
						{
							Name:  "Autocomplete 4 second option",
							Value: "autocomplete_1_default",
						},
						{
							Name:  "Choice 3.1",
							Value: "choice3_1",
						},
						{
							Name:  "Choice 4.1",
							Value: "choice4_1",
						},
						{
							Name:  "Choice 5.1",
							Value: "choice5_1",
						},
					}
					if data.Options[1].StringValue() != "" {
						choices = append(choices, &dgo.ApplicationCommandOptionChoice{
							Name:  data.Options[1].StringValue(),
							Value: "choice_custom_2",
						})
					}
				}

				err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
					Type: dgo.InteractionApplicationCommandAutocompleteResult,
					Data: &dgo.InteractionResponseData{
						Choices: choices,
					},
				})
				if err != nil {
					panic(err)
				}
			}
		},
	}
)

func main() {
	s.AddHandler(func(s *dgo.Session, r *dgo.Ready) { log.Println("Bot is up!") })
	s.AddHandler(func(s *dgo.Session, i *dgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})
	err := s.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}
	defer s.Close()

	createdCommands, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, *GuildID, commands)

	if err != nil {
		log.Fatalf("Cannot register commands: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Gracefully shutting down")

	if *RemoveCommands {
		for _, cmd := range createdCommands {
			err := s.ApplicationCommandDelete(s.State.User.ID, *GuildID, cmd.ID)
			if err != nil {
				log.Fatalf("Cannot delete %q command: %v", cmd.Name, err)
			}
		}
	}
}
