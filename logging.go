// Discordgo - Discord bindings for Go
// Available at https://github.com/darui3018823/dgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains code related to discordgo package logging

package dgo

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"runtime"
	"strings"
)

const (

	// LogError level is used for critical errors that could lead to data loss
	// or panic that would not be returned to a calling function.
	LogError int = iota

	// LogWarning level is used for very abnormal events and errors that are
	// also returned to a calling function.
	LogWarning

	// LogInformational level is used for normal non-error activity
	LogInformational

	// LogDebug level is for very detailed non-error activity.  This is
	// very spammy and will impact performance.
	LogDebug
)

// Logger can be used to replace the standard logging for discordgo
var Logger func(msgL, caller int, format string, a ...interface{})

const redactedValue = "[REDACTED]"

func isSensitiveLogKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		key == "authorization" ||
		key == "session_id" ||
		key == "code"
}

func redactJSONValue(value interface{}) interface{} {
	switch value := value.(type) {
	case map[string]interface{}:
		for key, item := range value {
			if isSensitiveLogKey(key) {
				// Discord API error codes are numeric and safe to retain. String
				// authorization codes remain sensitive and are redacted.
				if strings.EqualFold(key, "code") {
					if _, isString := item.(string); !isString {
						value[key] = redactJSONValue(item)
						continue
					}
				}
				value[key] = redactedValue
			} else {
				value[key] = redactJSONValue(item)
			}
		}
	case []interface{}:
		for index, item := range value {
			value[index] = redactJSONValue(item)
		}
	case string:
		if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return sanitizeURL(value)
		}
	}
	return value
}

func redactJSON(data []byte) string {
	if len(data) == 0 {
		return "<empty>"
	}

	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return "<redacted non-JSON payload>"
	}
	value = redactJSONValue(value)
	redacted, err := json.Marshal(value)
	if err != nil {
		return "<redacted JSON payload>"
	}
	return string(redacted)
}

func sanitizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid URL>"
	}
	parsed.User = nil

	segments := strings.Split(parsed.EscapedPath(), "/")
	for index, segment := range segments {
		switch strings.ToLower(segment) {
		case "webhooks", "interactions":
			tokenIndex := index + 2
			if tokenIndex < len(segments) && segments[tokenIndex] != "" {
				segments[tokenIndex] = redactedValue
			}
		}
	}
	parsed.RawPath = strings.Join(segments, "/")
	if path, err := url.PathUnescape(parsed.RawPath); err == nil {
		parsed.Path = path
	}

	query := parsed.Query()
	for key := range query {
		if isSensitiveLogKey(key) {
			query.Set(key, redactedValue)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// msglog provides package wide logging consistency for discordgo
// the format, a...  portion this command follows that of fmt.Printf
//
//	msgL   : LogLevel of the message
//	caller : 1 + the number of callers away from the message source
//	format : Printf style message format
//	a ...  : comma separated list of values to pass
func msglog(msgL, caller int, format string, a ...interface{}) {

	if Logger != nil {
		Logger(msgL, caller, format, a...)
	} else {

		pc, file, line, _ := runtime.Caller(caller)

		files := strings.Split(file, "/")
		file = files[len(files)-1]

		name := runtime.FuncForPC(pc).Name()
		fns := strings.Split(name, ".")
		name = fns[len(fns)-1]

		msg := fmt.Sprintf(format, a...)

		log.Printf("[DG%d] %s:%d:%s() %s\n", msgL, file, line, name, msg)
	}
}

// SetLogger safely replaces the structured logger used by the session.
// A nil logger disables session logging.
func (s *Session) SetLogger(logger *slog.Logger) {
	s.loggerMu.Lock()
	s.Logger = logger
	s.loggerMu.Unlock()
}

func (s *Session) logger() *slog.Logger {
	s.loggerMu.RLock()
	logger := s.Logger
	s.loggerMu.RUnlock()
	return logger
}

// log writes through the session's slog logger. Filtering is delegated to the
// configured slog.Handler.
func (s *Session) log(msgL int, format string, a ...interface{}) {
	logger := s.logger()
	if logger == nil {
		return
	}

	msg := fmt.Sprintf(format, a...)

	// Map old integer levels to slog levels
	switch msgL {
	case LogError:
		logger.Error(msg)
	case LogWarning:
		logger.Warn(msg)
	case LogInformational:
		logger.Info(msg)
	case LogDebug:
		logger.Debug(msg)
	default:
		logger.Info(msg)
	}
}

// helper function that wraps msglog for the VoiceConnection struct
// This adds a check to insure the message is only logged
// if the voice connection log level is equal or higher than the
// message log level
func (v *VoiceConnection) log(msgL int, format string, a ...interface{}) {

	if msgL > v.LogLevel {
		return
	}

	msglog(msgL, 2, format, a...)
}

// printJSON is a helper function to display JSON data in an easy to read format.
/* NOT USED ATM
func printJSON(body []byte) {
	var prettyJSON bytes.Buffer
	error := json.Indent(&prettyJSON, body, "", "\t")
	if error != nil {
		log.Print("JSON parse error: ", error)
	}
	log.Println(string(prettyJSON.Bytes()))
}
*/
