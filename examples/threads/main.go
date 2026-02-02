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

// Flags
var (
	BotToken = flag.String("token", "", "Bot token")
)

const timeout time.Duration = time.Second * 10

var games map[string]time.Time = make(map[string]time.Time)

func init() { flag.Parse() }

func main() {
	s, _ := dgo.New("Bot " + *BotToken)
	s.AddHandler(func(s *dgo.Session, r *dgo.Ready) {
		fmt.Println("Bot is ready")
	})
	s.AddHandler(func(s *dgo.Session, m *dgo.MessageCreate) {
		if strings.Contains(m.Content, "ping") {
			if ch, err := s.State.Channel(m.ChannelID); err != nil || !ch.IsThread() {
				thread, err := s.MessageThreadStartComplex(m.ChannelID, m.ID, &dgo.ThreadStart{
					Name:                "Pong game with " + m.Author.Username,
					AutoArchiveDuration: 60,
					Invitable:           false,
					RateLimitPerUser:    10,
				})
				if err != nil {
					panic(err)
				}
				_, _ = s.ChannelMessageSend(thread.ID, "pong")
				m.ChannelID = thread.ID
			} else {
				_, _ = s.ChannelMessageSendReply(m.ChannelID, "pong", m.Reference())
			}
			games[m.ChannelID] = time.Now()
			<-time.After(timeout)
			if time.Since(games[m.ChannelID]) >= timeout {
				archived := true
				locked := true
				_, err := s.ChannelEditComplex(m.ChannelID, &dgo.ChannelEdit{
					Archived: &archived,
					Locked:   &locked,
				})
				if err != nil {
					panic(err)
				}
			}
		}
	})
	s.Identify.Intents = dgo.MakeIntent(dgo.IntentsAllWithoutPrivileged)

	err := s.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}
	defer s.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Graceful shutdown")

}
