package dgo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrMultipartBodyNotReplayable is returned when an HTTP retry needs a fresh
// copy of a one-shot file reader.
var ErrMultipartBodyNotReplayable = errors.New("multipart file reader cannot be replayed")

// MultipartRequestBody is a streaming, reopenable multipart body.
type MultipartRequestBody struct {
	contentType string
	parts       []multipartRequestPart
}

type multipartRequestPart struct {
	prefix []byte
	source *multipartFileSource
}

type multipartFileSource struct {
	open func() (io.ReadCloser, error)
}

type multipartReader struct {
	io.Reader
	closers []io.Closer
}

func (r *multipartReader) Close() error {
	var result error
	for i := len(r.closers) - 1; i >= 0; i-- {
		if err := r.closers[i].Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}

type unlockReadCloser struct {
	io.Reader
	unlock func()
}

func (r *unlockReadCloser) Close() error {
	if r.unlock != nil {
		r.unlock()
		r.unlock = nil
	}
	return nil
}

// ContentType returns the multipart/form-data content type, including its
// boundary.
func (b *MultipartRequestBody) ContentType() string {
	if b == nil {
		return ""
	}
	return b.contentType
}

// Open creates a fresh streaming body for one HTTP attempt.
func (b *MultipartRequestBody) Open() (io.ReadCloser, error) {
	if b == nil {
		return nil, fmt.Errorf("multipart body is nil")
	}
	readers := make([]io.Reader, 0, len(b.parts)*2)
	closers := make([]io.Closer, 0, len(b.parts))
	for _, part := range b.parts {
		if len(part.prefix) > 0 {
			readers = append(readers, bytes.NewReader(part.prefix))
		}
		if part.source == nil {
			continue
		}
		reader, err := part.source.open()
		if err != nil {
			for i := len(closers) - 1; i >= 0; i-- {
				_ = closers[i].Close()
			}
			return nil, err
		}
		if reader == nil {
			for i := len(closers) - 1; i >= 0; i-- {
				_ = closers[i].Close()
			}
			return nil, fmt.Errorf("multipart file opener returned a nil reader")
		}
		readers = append(readers, reader)
		closers = append(closers, reader)
	}
	return &multipartReader{
		Reader:  io.MultiReader(readers...),
		closers: closers,
	}, nil
}

// NewMultipartBodyWithJSON creates a streaming multipart body containing a
// payload_json part and zero or more file parts.
func NewMultipartBodyWithJSON(data interface{}, files []*File) (*MultipartRequestBody, error) {
	payload, err := Marshal(data)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="payload_json"`)
	h.Set("Content-Type", "application/json")
	payloadPart, err := writer.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := payloadPart.Write(payload); err != nil {
		return nil, err
	}

	body := &MultipartRequestBody{contentType: writer.FormDataContentType()}
	for i, file := range files {
		if file == nil {
			return nil, fmt.Errorf("file %d can not be nil", i)
		}
		source, err := newMultipartFileSource(file)
		if err != nil {
			return nil, fmt.Errorf("file %d: %w", i, err)
		}
		h := make(textproto.MIMEHeader)
		h.Set(
			"Content-Disposition",
			fmt.Sprintf(`form-data; name="files[%d]"; filename="%s"`, i, quoteEscaper.Replace(file.Name)),
		)
		contentType := file.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		h.Set("Content-Type", contentType)
		if _, err := writer.CreatePart(h); err != nil {
			return nil, err
		}
		body.parts = append(body.parts, multipartRequestPart{
			prefix: append([]byte(nil), buffer.Bytes()...),
			source: source,
		})
		buffer.Reset()
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	body.parts = append(body.parts, multipartRequestPart{
		prefix: append([]byte(nil), buffer.Bytes()...),
	})
	return body, nil
}

// NewMultipartBodyWithFieldsAndFile creates a streaming multipart body for
// plain form fields and one file part.
func NewMultipartBodyWithFieldsAndFile(fields map[string]string, fileFieldName string, file *File) (*MultipartRequestBody, error) {
	if file == nil {
		return nil, fmt.Errorf("file can not be nil")
	}
	source, err := newMultipartFileSource(file)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := writer.WriteField(key, fields[key]); err != nil {
			return nil, err
		}
	}

	h := make(textproto.MIMEHeader)
	h.Set(
		"Content-Disposition",
		fmt.Sprintf(
			`form-data; name="%s"; filename="%s"`,
			quoteEscaper.Replace(fileFieldName),
			quoteEscaper.Replace(file.Name),
		),
	)
	contentType := file.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	h.Set("Content-Type", contentType)
	if _, err := writer.CreatePart(h); err != nil {
		return nil, err
	}

	body := &MultipartRequestBody{
		contentType: writer.FormDataContentType(),
		parts: []multipartRequestPart{{
			prefix: append([]byte(nil), buffer.Bytes()...),
			source: source,
		}},
	}
	buffer.Reset()
	if err := writer.Close(); err != nil {
		return nil, err
	}
	body.parts = append(body.parts, multipartRequestPart{
		prefix: append([]byte(nil), buffer.Bytes()...),
	})
	return body, nil
}

func newMultipartFileSource(file *File) (*multipartFileSource, error) {
	if file.Open != nil {
		return &multipartFileSource{open: file.Open}, nil
	}
	if file.Reader == nil {
		return nil, fmt.Errorf("file reader can not be nil")
	}
	if buffer, ok := file.Reader.(*bytes.Buffer); ok {
		data := buffer.Bytes()
		return &multipartFileSource{open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}}, nil
	}
	if seeker, ok := file.Reader.(io.ReadSeeker); ok {
		offset, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		var mu sync.Mutex
		return &multipartFileSource{open: func() (io.ReadCloser, error) {
			mu.Lock()
			if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
				mu.Unlock()
				return nil, err
			}
			return &unlockReadCloser{Reader: seeker, unlock: mu.Unlock}, nil
		}}, nil
	}

	var mu sync.Mutex
	used := false
	return &multipartFileSource{open: func() (io.ReadCloser, error) {
		mu.Lock()
		defer mu.Unlock()
		if used {
			return nil, ErrMultipartBodyNotReplayable
		}
		used = true
		return io.NopCloser(file.Reader), nil
	}}, nil
}

// SnowflakeTimestamp returns the creation time of a Snowflake ID relative to the creation of Discord.
func SnowflakeTimestamp(ID string) (t time.Time, err error) {
	i, err := strconv.ParseInt(ID, 10, 64)
	if err != nil {
		return
	}
	timestamp := (i >> 22) + 1420070400000
	t = time.Unix(0, timestamp*1000000)
	return
}

// MultipartBodyWithJSON returns the contentType and buffered body for a Discord request.
//
// Deprecated: use NewMultipartBodyWithJSON and RequestRawWithBody to stream
// attachments without retaining them in memory.
// data  : The object to encode for payload_json in the multipart request
// files : Files to include in the request
func MultipartBodyWithJSON(data interface{}, files []*File) (requestContentType string, requestBody []byte, err error) {
	body, err := NewMultipartBodyWithJSON(data, files)
	if err != nil {
		return "", nil, err
	}
	reader, err := body.Open()
	if err != nil {
		return "", nil, err
	}
	defer reader.Close()
	requestBody, err = io.ReadAll(reader)
	return body.ContentType(), requestBody, err
}

// MultipartBodyWithFieldsAndFile returns a buffered multipart body.
//
// Deprecated: use NewMultipartBodyWithFieldsAndFile and RequestRawWithBody.
func MultipartBodyWithFieldsAndFile(fields map[string]string, fileFieldName string, file *File) (requestContentType string, requestBody []byte, err error) {
	body, err := NewMultipartBodyWithFieldsAndFile(fields, fileFieldName, file)
	if err != nil {
		return "", nil, err
	}
	reader, err := body.Open()
	if err != nil {
		return "", nil, err
	}
	defer reader.Close()
	requestBody, err = io.ReadAll(reader)
	return body.ContentType(), requestBody, err
}

func avatarURL(avatarHash, defaultAvatarURL, staticAvatarURL, animatedAvatarURL, size string) string {
	var URL string
	if avatarHash == "" {
		URL = defaultAvatarURL
	} else if strings.HasPrefix(avatarHash, "a_") {
		URL = animatedAvatarURL
	} else {
		URL = staticAvatarURL
	}

	if size != "" {
		return URL + "?size=" + size
	}
	return URL
}

func bannerURL(bannerHash, staticBannerURL, animatedBannerURL, size string) string {
	var URL string
	if bannerHash == "" {
		return ""
	} else if strings.HasPrefix(bannerHash, "a_") {
		URL = animatedBannerURL
	} else {
		URL = staticBannerURL
	}

	if size != "" {
		return URL + "?size=" + size
	}
	return URL
}

func iconURL(iconHash, staticIconURL, animatedIconURL, size string) string {
	var URL string
	if iconHash == "" {
		return ""
	} else if strings.HasPrefix(iconHash, "a_") {
		URL = animatedIconURL
	} else {
		URL = staticIconURL
	}

	if size != "" {
		return URL + "?size=" + size
	}
	return URL
}
