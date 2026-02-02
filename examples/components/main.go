package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/darui3018823/dgo"
)

// Bot parameters
var (
	GuildID  = flag.String("guild", "", "Test guild ID")
	BotToken = flag.String("token", "", "Bot access token")
	AppID    = flag.String("app", "", "Application ID")
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

// Important note: call every command in order it's placed in the example.

var (
	componentsHandlers = map[string]func(s *dgo.Session, i *dgo.InteractionCreate){
		"fd_no": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: "Huh. I see, maybe some of these resources might help you?",
					Flags:   dgo.MessageFlagsEphemeral,
					Components: []dgo.MessageComponent{
						dgo.ActionsRow{
							Components: []dgo.MessageComponent{
								dgo.Button{
									Emoji: &dgo.ComponentEmoji{
										Name: "📜",
									},
									Label: "Documentation",
									Style: dgo.LinkButton,
									URL:   "https://discord.com/developers/docs/interactions/message-components#buttons",
								},
								dgo.Button{
									Emoji: &dgo.ComponentEmoji{
										Name: "🔧",
									},
									Label: "Discord developers",
									Style: dgo.LinkButton,
									URL:   "https://discord.gg/discord-developers",
								},
								dgo.Button{
									Emoji: &dgo.ComponentEmoji{
										Name: "🦫",
									},
									Label: "Discord Gophers",
									Style: dgo.LinkButton,
									URL:   "https://discord.gg/7RuRrVHyXF",
								},
							},
						},
					},
				},
			})
			if err != nil {
				panic(err)
			}
		},
		"fd_yes": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: "Great! If you wanna know more or just have questions, feel free to visit Discord Devs and Discord Gophers server. " +
						"But now, when you know how buttons work, let's move onto select menus (execute `/selects single`)",
					Flags: dgo.MessageFlagsEphemeral,
					Components: []dgo.MessageComponent{
						dgo.ActionsRow{
							Components: []dgo.MessageComponent{
								dgo.Button{
									Emoji: &dgo.ComponentEmoji{
										Name: "🔧",
									},
									Label: "Discord developers",
									Style: dgo.LinkButton,
									URL:   "https://discord.gg/discord-developers",
								},
								dgo.Button{
									Emoji: &dgo.ComponentEmoji{
										Name: "🦫",
									},
									Label: "Discord Gophers",
									Style: dgo.LinkButton,
									URL:   "https://discord.gg/7RuRrVHyXF",
								},
							},
						},
					},
				},
			})
			if err != nil {
				panic(err)
			}
		},
		"select": func(s *dgo.Session, i *dgo.InteractionCreate) {
			var response *dgo.InteractionResponse

			data := i.MessageComponentData()
			switch data.Values[0] {
			case "go":
				response = &dgo.InteractionResponse{
					Type: dgo.InteractionResponseChannelMessageWithSource,
					Data: &dgo.InteractionResponseData{
						Content: "This is the way.",
						Flags:   dgo.MessageFlagsEphemeral,
					},
				}
			default:
				response = &dgo.InteractionResponse{
					Type: dgo.InteractionResponseChannelMessageWithSource,
					Data: &dgo.InteractionResponseData{
						Content: "It is not the way to go.",
						Flags:   dgo.MessageFlagsEphemeral,
					},
				}
			}
			err := s.InteractionRespond(i.Interaction, response)
			if err != nil {
				panic(err)
			}
			time.Sleep(time.Second) // Doing that so user won't see instant response.
			_, err = s.FollowupMessageCreate(i.Interaction, true, &dgo.WebhookParams{
				Content: "Anyways, now when you know how to use single select menus, let's see how multi select menus work. " +
					"Try calling `/selects multi` command.",
				Flags: dgo.MessageFlagsEphemeral,
			})
			if err != nil {
				panic(err)
			}
		},
		"stackoverflow_tags": func(s *dgo.Session, i *dgo.InteractionCreate) {
			data := i.MessageComponentData()

			const stackoverflowFormat = `https://stackoverflow.com/questions/tagged/%s`

			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: "Here is your stackoverflow URL: " + fmt.Sprintf(stackoverflowFormat, strings.Join(data.Values, "+")),
					Flags:   dgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				panic(err)
			}
			time.Sleep(time.Second) // Doing that so user won't see instant response.
			_, err = s.FollowupMessageCreate(i.Interaction, true, &dgo.WebhookParams{
				Content: "But wait, there is more! You can also auto populate the select menu. Try executing `/selects auto-populated`.",
				Flags:   dgo.MessageFlagsEphemeral,
			})
			if err != nil {
				panic(err)
			}
		},
		"channel_select": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: "This is it. You've reached your destination. Your choice was <#" + i.MessageComponentData().Values[0] + ">\n" +
						"If you want to know more, check out the links below",
					Components: []dgo.MessageComponent{
						dgo.ActionsRow{
							Components: []dgo.MessageComponent{
								dgo.Button{
									Emoji: &dgo.ComponentEmoji{
										Name: "📜",
									},
									Label: "Documentation",
									Style: dgo.LinkButton,
									URL:   "https://discord.com/developers/docs/interactions/message-components#select-menus",
								},
								dgo.Button{
									Emoji: &dgo.ComponentEmoji{
										Name: "🔧",
									},
									Label: "Discord developers",
									Style: dgo.LinkButton,
									URL:   "https://discord.gg/discord-developers",
								},
								dgo.Button{
									Emoji: &dgo.ComponentEmoji{
										Name: "🦫",
									},
									Label: "Discord Gophers",
									Style: dgo.LinkButton,
									URL:   "https://discord.gg/7RuRrVHyXF",
								},
							},
						},
					},

					Flags: dgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				panic(err)
			}
		},
	}
	commandsHandlers = map[string]func(s *dgo.Session, i *dgo.InteractionCreate){
		"buttons": func(s *dgo.Session, i *dgo.InteractionCreate) {
			err := s.InteractionRespond(i.Interaction, &dgo.InteractionResponse{
				Type: dgo.InteractionResponseChannelMessageWithSource,
				Data: &dgo.InteractionResponseData{
					Content: "Are you comfortable with buttons and other message components?",
					Flags:   dgo.MessageFlagsEphemeral,
					// Buttons and other components are specified in Components field.
					Components: []dgo.MessageComponent{
						// ActionRow is a container of all buttons within the same row.
						dgo.ActionsRow{
							Components: []dgo.MessageComponent{
								dgo.Button{
									// Label is what the user will see on the button.
									Label: "Yes",
									// Style provides coloring of the button. There are not so many styles tho.
									Style: dgo.SuccessButton,
									// Disabled allows bot to disable some buttons for users.
									Disabled: false,
									// CustomID is a thing telling Discord which data to send when this button will be pressed.
									CustomID: "fd_yes",
								},
								dgo.Button{
									Label:    "No",
									Style:    dgo.DangerButton,
									Disabled: false,
									CustomID: "fd_no",
								},
								dgo.Button{
									Label:    "I don't know",
									Style:    dgo.LinkButton,
									Disabled: false,
									// Link buttons don't require CustomID and do not trigger the gateway/HTTP event
									URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
									Emoji: &dgo.ComponentEmoji{
										Name: "🤷",
									},
								},
							},
						},
						// The message may have multiple actions rows.
						dgo.ActionsRow{
							Components: []dgo.MessageComponent{
								dgo.Button{
									Label:    "Discord Developers server",
									Style:    dgo.LinkButton,
									Disabled: false,
									URL:      "https://discord.gg/discord-developers",
								},
							},
						},
					},
				},
			})
			if err != nil {
				panic(err)
			}
		},
		"selects": func(s *dgo.Session, i *dgo.InteractionCreate) {
			var response *dgo.InteractionResponse
			switch i.ApplicationCommandData().Options[0].Name {
			case "single":
				response = &dgo.InteractionResponse{
					Type: dgo.InteractionResponseChannelMessageWithSource,
					Data: &dgo.InteractionResponseData{
						Content: "Now let's take a look on selects. This is single item select menu.",
						Flags:   dgo.MessageFlagsEphemeral,
						Components: []dgo.MessageComponent{
							dgo.ActionsRow{
								Components: []dgo.MessageComponent{
									dgo.SelectMenu{
										// Select menu, as other components, must have a customID, so we set it to this value.
										CustomID:    "select",
										Placeholder: "Choose your favorite programming language 👇",
										Options: []dgo.SelectMenuOption{
											{
												Label: "Go",
												// As with components, this things must have their own unique "id" to identify which is which.
												// In this case such id is Value field.
												Value: "go",
												Emoji: &dgo.ComponentEmoji{
													Name: "🦦",
												},
												// You can also make it a default option, but in this case we won't.
												Default:     false,
												Description: "Go programming language",
											},
											{
												Label: "JS",
												Value: "js",
												Emoji: &dgo.ComponentEmoji{
													Name: "🟨",
												},
												Description: "JavaScript programming language",
											},
											{
												Label: "Python",
												Value: "py",
												Emoji: &dgo.ComponentEmoji{
													Name: "🐍",
												},
												Description: "Python programming language",
											},
										},
									},
								},
							},
						},
					},
				}
			case "multi":
				minValues := 1
				response = &dgo.InteractionResponse{
					Type: dgo.InteractionResponseChannelMessageWithSource,
					Data: &dgo.InteractionResponseData{
						Content: "Now let's see how the multi-item select menu works: " +
							"try generating your own stackoverflow search link",
						Flags: dgo.MessageFlagsEphemeral,
						Components: []dgo.MessageComponent{
							dgo.ActionsRow{
								Components: []dgo.MessageComponent{
									dgo.SelectMenu{
										CustomID:    "stackoverflow_tags",
										Placeholder: "Select tags to search on StackOverflow",
										// This is where confusion comes from. If you don't specify these things you will get single item select.
										// These fields control the minimum and maximum amount of selected items.
										MinValues: &minValues,
										MaxValues: 3,
										Options: []dgo.SelectMenuOption{
											{
												Label:       "Go",
												Description: "Simple yet powerful programming language",
												Value:       "go",
												// Default works the same for multi-select menus.
												Default: false,
												Emoji: &dgo.ComponentEmoji{
													Name: "🦦",
												},
											},
											{
												Label:       "JS",
												Description: "Multiparadigm OOP language",
												Value:       "javascript",
												Emoji: &dgo.ComponentEmoji{
													Name: "🟨",
												},
											},
											{
												Label:       "Python",
												Description: "OOP prototyping programming language",
												Value:       "python",
												Emoji: &dgo.ComponentEmoji{
													Name: "🐍",
												},
											},
											{
												Label:       "Web",
												Description: "Web related technologies",
												Value:       "web",
												Emoji: &dgo.ComponentEmoji{
													Name: "🌐",
												},
											},
											{
												Label:       "Desktop",
												Description: "Desktop applications",
												Value:       "desktop",
												Emoji: &dgo.ComponentEmoji{
													Name: "💻",
												},
											},
										},
									},
								},
							},
						},
					},
				}
			case "auto-populated":
				response = &dgo.InteractionResponse{
					Type: dgo.InteractionResponseChannelMessageWithSource,
					Data: &dgo.InteractionResponseData{
						Content: "The tastiest things are left for the end. Meet auto populated select menus.\n" +
							"By setting `MenuType` on the select menu you can tell Discord to automatically populate the menu with entities of your choice: roles, members, channels. Try one below.",
						Flags: dgo.MessageFlagsEphemeral,
						Components: []dgo.MessageComponent{
							dgo.ActionsRow{
								Components: []dgo.MessageComponent{
									dgo.SelectMenu{
										MenuType:     dgo.ChannelSelectMenu,
										CustomID:     "channel_select",
										Placeholder:  "Pick your favorite channel!",
										ChannelTypes: []dgo.ChannelType{dgo.ChannelTypeGuildText},
									},
								},
							},
						},
					},
				}
			}
			err := s.InteractionRespond(i.Interaction, response)
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
	// Components are part of interactions, so we register InteractionCreate handler
	s.AddHandler(func(s *dgo.Session, i *dgo.InteractionCreate) {
		switch i.Type {
		case dgo.InteractionApplicationCommand:
			if h, ok := commandsHandlers[i.ApplicationCommandData().Name]; ok {
				h(s, i)
			}
		case dgo.InteractionMessageComponent:

			if h, ok := componentsHandlers[i.MessageComponentData().CustomID]; ok {
				h(s, i)
			}
		}
	})
	_, err := s.ApplicationCommandCreate(*AppID, *GuildID, &dgo.ApplicationCommand{
		Name:        "buttons",
		Description: "Test the buttons if you got courage",
	})

	if err != nil {
		log.Fatalf("Cannot create slash command: %v", err)
	}
	_, err = s.ApplicationCommandCreate(*AppID, *GuildID, &dgo.ApplicationCommand{
		Name: "selects",
		Options: []*dgo.ApplicationCommandOption{
			{
				Type:        dgo.ApplicationCommandOptionSubCommand,
				Name:        "multi",
				Description: "Multi-item select menu",
			},
			{
				Type:        dgo.ApplicationCommandOptionSubCommand,
				Name:        "single",
				Description: "Single-item select menu",
			},
			{
				Type:        dgo.ApplicationCommandOptionSubCommand,
				Name:        "auto-populated",
				Description: "Automatically populated select menu, which lets you pick a member, channel or role",
			},
		},
		Description: "Lo and behold: dropdowns are coming",
	})

	if err != nil {
		log.Fatalf("Cannot create slash command: %v", err)
	}

	err = s.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}
	defer s.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Graceful shutdown")
}
