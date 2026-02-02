package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/darui3018823/dgo"
)

// Flags
var (
	GuildID        = flag.String("guild", "", "Test guild ID")
	VoiceChannelID = flag.String("voice", "", "Test voice channel ID")
	BotToken       = flag.String("token", "", "Bot token")
)

func init() { flag.Parse() }

func main() {
	s, _ := dgo.New("Bot " + *BotToken)
	s.AddHandler(func(s *dgo.Session, r *dgo.Ready) {
		fmt.Println("Bot is ready")
	})

	err := s.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}
	defer s.Close()

	event := createAmazingEvent(s)
	transformEventToExternalEvent(s, event)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Graceful shutdown")

}

// Create a new event on guild
func createAmazingEvent(s *dgo.Session) *dgo.GuildScheduledEvent {
	// Define the starting time (must be in future)
	startingTime := time.Now().Add(1 * time.Hour)
	// Define the ending time (must be after starting time)
	endingTime := startingTime.Add(30 * time.Minute)
	// Create the event
	scheduledEvent, err := s.GuildScheduledEventCreate(*GuildID, &dgo.GuildScheduledEventParams{
		Name:               "Amazing Event",
		Description:        "This event will start in 1 hour and last 30 minutes",
		ScheduledStartTime: &startingTime,
		ScheduledEndTime:   &endingTime,
		EntityType:         dgo.GuildScheduledEventEntityTypeVoice,
		ChannelID:          *VoiceChannelID,
		PrivacyLevel:       dgo.GuildScheduledEventPrivacyLevelGuildOnly,
	})
	if err != nil {
		log.Printf("Error creating scheduled event: %v", err)
		return nil
	}

	fmt.Println("Created scheduled event:", scheduledEvent.Name)
	return scheduledEvent
}

func transformEventToExternalEvent(s *dgo.Session, event *dgo.GuildScheduledEvent) {
	scheduledEvent, err := s.GuildScheduledEventEdit(*GuildID, event.ID, &dgo.GuildScheduledEventParams{
		Name:       "Amazing Event @ Discord Website",
		EntityType: dgo.GuildScheduledEventEntityTypeExternal,
		EntityMetadata: &dgo.GuildScheduledEventEntityMetadata{
			Location: "https://discord.com",
		},
	})
	if err != nil {
		log.Printf("Error during transformation of scheduled voice event into external event: %v", err)
		return
	}

	fmt.Println("Created scheduled event:", scheduledEvent.Name)
}
