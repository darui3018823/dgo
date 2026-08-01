package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/darui3018823/dgo"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
)

// Variables used for command line parameters
var (
	Token       string
	ChannelID   string
	GuildID     string
	RecordAudio bool
)

func init() {
	flag.StringVar(&Token, "t", "", "Bot token")
	flag.StringVar(&GuildID, "g", "", "Guild in which voice channel exists")
	flag.StringVar(&ChannelID, "c", "", "Voice channel to connect to")
	flag.BoolVar(&RecordAudio, "record", false, "Record received audio to per-SSRC Ogg files (requires consent and a retention policy)")
}

func createPionRTPPacket(p *dgo.Packet) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			Version: 2,
			// Taken from Discord voice docs
			PayloadType:    0x78,
			SequenceNumber: p.Sequence,
			Timestamp:      p.Timestamp,
			SSRC:           p.SSRC,
		},
		Payload: p.Opus,
	}
}

type voiceWriterFactory func(ssrc uint32) (media.Writer, error)

func newOggWriter(ssrc uint32) (media.Writer, error) {
	return oggwriter.New(fmt.Sprintf("%d.ogg", ssrc), 48000, 2)
}

func handleVoice(c <-chan *dgo.Packet, record bool, writerFactory voiceWriterFactory) {
	if !record {
		for range c {
			// Receiving voice does not imply permission to persist it. Recording
			// therefore requires the explicit -record opt-in.
		}
		return
	}

	files := make(map[uint32]media.Writer)
	defer func() {
		for _, file := range files {
			if err := file.Close(); err != nil {
				fmt.Printf("failed to close recording: %v\n", err)
			}
		}
	}()

	for p := range c {
		if p == nil {
			continue
		}
		file, ok := files[p.SSRC]
		if !ok {
			var err error
			file, err = writerFactory(p.SSRC)
			if err != nil {
				fmt.Printf("failed to create file %d.ogg, giving up on recording: %v\n", p.SSRC, err)
				return
			}
			files[p.SSRC] = file
		}
		// Construct a pion RTP packet from dgo's type.
		rtp := createPionRTPPacket(p)
		err := file.WriteRTP(rtp)
		if err != nil {
			fmt.Printf("failed to write to file %d.ogg, giving up on recording: %v\n", p.SSRC, err)
		}
	}
}

func main() {
	flag.Parse()

	if RecordAudio {
		fmt.Println("WARNING: recording is enabled; obtain explicit consent, notify participants, and apply a lawful retention and deletion policy")
	} else {
		fmt.Println("Recording is disabled; received audio will not be written to files. Use -record only after satisfying the README safety requirements.")
	}

	s, err := dgo.NewBot(Token)
	if err != nil {
		fmt.Println("error creating Discord session:", err)
		return
	}
	defer s.Close()

	// We only really care about receiving voice state updates.
	s.Identify.Intents = dgo.MakeIntent(dgo.IntentsGuildVoiceStates)

	err = s.Open()
	if err != nil {
		fmt.Println("error opening connection:", err)
		return
	}

	v, err := s.ChannelVoiceJoin(GuildID, ChannelID, true, false)
	if err != nil {
		fmt.Println("failed to join voice channel:", err)
		return
	}

	go func() {
		time.Sleep(10 * time.Second)
		close(v.OpusRecv)
		v.Close()
	}()

	handleVoice(v.OpusRecv, RecordAudio, newOggWriter)
}
