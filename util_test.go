package dgo

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
	"time"
)

type countingReader struct {
	reader io.Reader
	reads  int
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	r.reads++
	return r.reader.Read(buffer)
}

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

func TestMultipartRequestBodyStreamsFiles(t *testing.T) {
	fileReader := &countingReader{reader: strings.NewReader("STREAMED-FILE")}
	body, err := NewMultipartBodyWithJSON(
		map[string]string{"content": "hello"},
		[]*File{{
			Name:        "stream.txt",
			ContentType: "text/plain",
			Reader:      fileReader,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if fileReader.reads != 0 {
		t.Fatalf("file was read while constructing multipart body: %d reads", fileReader.reads)
	}

	stream, err := body.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if fileReader.reads != 0 {
		t.Fatalf("file was read while opening multipart body: %d reads", fileReader.reads)
	}

	_, params, err := mime.ParseMediaType(body.ContentType())
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(stream, params["boundary"])
	payloadPart, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(payloadPart)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"content":"hello"}` {
		t.Fatalf("payload_json = %q", payload)
	}
	filePart, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	fileData, err := io.ReadAll(filePart)
	if err != nil {
		t.Fatal(err)
	}
	if string(fileData) != "STREAMED-FILE" {
		t.Fatalf("file data = %q", fileData)
	}
	if fileReader.reads == 0 {
		t.Fatal("file was never streamed")
	}
}

func TestMultipartRequestBodyReplayContract(t *testing.T) {
	opens := 0
	body, err := NewMultipartBodyWithJSON(
		map[string]string{"content": "hello"},
		[]*File{{
			Name: "replay.txt",
			Open: func() (io.ReadCloser, error) {
				opens++
				return io.NopCloser(strings.NewReader("REPLAY")), nil
			},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	var attempts [][]byte
	for i := 0; i < 2; i++ {
		stream, err := body.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(stream)
		closeErr := stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		attempts = append(attempts, data)
	}
	if opens != 2 {
		t.Fatalf("file opens = %d, want 2", opens)
	}
	if !bytes.Equal(attempts[0], attempts[1]) {
		t.Fatal("replayed multipart bodies differ")
	}

	nonReplayable, err := NewMultipartBodyWithJSON(
		map[string]string{"content": "hello"},
		[]*File{{
			Name:   "once.txt",
			Reader: &countingReader{reader: strings.NewReader("ONCE")},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := nonReplayable.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := nonReplayable.Open(); !errors.Is(err, ErrMultipartBodyNotReplayable) {
		t.Fatalf("second open error = %v, want ErrMultipartBodyNotReplayable", err)
	}
}
