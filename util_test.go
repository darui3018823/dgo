package dgo

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSnowflakeTimestamp(t *testing.T) {
	// #discordgo channel ID :)
	id := "155361364909621248"
	parsedTimestamp, err := SnowflakeTimestamp(id)

	if err != nil {
		t.Errorf("returned error incorrect: got %v, want nil", err)
	}

	correctTimestamp := time.Date(2016, time.March, 4, 17, 10, 35, 869*1000000, time.UTC)
	if !parsedTimestamp.Equal(correctTimestamp) {
		t.Errorf("parsed time incorrect: got %v, want %v", parsedTimestamp, correctTimestamp)
	}
}

func TestMultipartBodyWithFieldsAndFile(t *testing.T) {
	t.Run("file required", func(t *testing.T) {
		_, _, err := MultipartBodyWithFieldsAndFile(map[string]string{"name": "wave"}, "file", nil)
		if err == nil {
			t.Fatal("expected error when file is nil")
		}
	})

	t.Run("encodes fields and file", func(t *testing.T) {
		contentType, body, err := MultipartBodyWithFieldsAndFile(map[string]string{
			"name":        "wave",
			"description": "hello",
			"tags":        "wave",
		}, "file", &File{
			Name:        "wave.png",
			ContentType: "image/png",
			Reader:      bytes.NewBufferString("PNGDATA"),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.HasPrefix(contentType, "multipart/form-data;") {
			t.Fatalf("content type mismatch: %s", contentType)
		}

		bodyString := string(body)
		for _, expected := range []string{"name=\"file\"; filename=\"wave.png\"", "name=\"name\"", "wave", "name=\"description\"", "hello", "name=\"tags\"", "PNGDATA"} {
			if !strings.Contains(bodyString, expected) {
				t.Fatalf("multipart body missing %q", expected)
			}
		}
	})
}
