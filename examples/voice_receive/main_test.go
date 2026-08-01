package main

import (
	"testing"

	"github.com/darui3018823/dgo"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media"
)

type testVoiceWriter struct {
	writes int
	closes int
}

func (w *testVoiceWriter) WriteRTP(*rtp.Packet) error {
	w.writes++
	return nil
}

func (w *testVoiceWriter) Close() error {
	w.closes++
	return nil
}

func TestHandleVoiceDoesNotCreateWriterWithoutOptIn(t *testing.T) {
	packets := make(chan *dgo.Packet, 1)
	packets <- &dgo.Packet{SSRC: 42, Opus: []byte{1, 2, 3}}
	close(packets)

	factoryCalls := 0
	handleVoice(packets, false, func(uint32) (media.Writer, error) {
		factoryCalls++
		return &testVoiceWriter{}, nil
	})

	if factoryCalls != 0 {
		t.Fatalf("writer factory called %d times without recording opt-in", factoryCalls)
	}
}

func TestHandleVoiceCreatesWriterOnlyWithOptIn(t *testing.T) {
	packets := make(chan *dgo.Packet, 1)
	packets <- &dgo.Packet{
		Sequence:  7,
		Timestamp: 960,
		SSRC:      42,
		Opus:      []byte{1, 2, 3},
	}
	close(packets)

	writer := &testVoiceWriter{}
	factoryCalls := 0
	handleVoice(packets, true, func(ssrc uint32) (media.Writer, error) {
		factoryCalls++
		if ssrc != 42 {
			t.Fatalf("writer SSRC = %d, want 42", ssrc)
		}
		return writer, nil
	})

	if factoryCalls != 1 {
		t.Fatalf("writer factory called %d times, want 1", factoryCalls)
	}
	if writer.writes != 1 {
		t.Fatalf("writer received %d packets, want 1", writer.writes)
	}
	if writer.closes != 1 {
		t.Fatalf("writer closed %d times, want 1", writer.closes)
	}
}
