package dgo

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGroupDMInviteStickerEndpointBuilders(t *testing.T) {
	tests := map[string]struct {
		got  string
		want string
	}{
		"group DM recipient": {
			got:  EndpointChannelRecipient("channel", "user"),
			want: EndpointAPI + "channels/channel/recipients/user",
		},
		"invite target users": {
			got:  EndpointInviteTargetUsers("invite"),
			want: EndpointAPI + "invites/invite/target-users",
		},
		"invite target users job": {
			got:  EndpointInviteTargetUsersJobStatus("invite"),
			want: EndpointAPI + "invites/invite/target-users/job-status",
		},
		"sticker packs": {
			got:  EndpointStickerPacks,
			want: EndpointAPI + "sticker-packs",
		},
		"sticker pack": {
			got:  EndpointStickerPack("pack"),
			want: EndpointAPI + "sticker-packs/pack",
		},
		"legacy sticker packs alias": {
			got:  EndpointNitroStickersPacks,
			want: EndpointAPI + "sticker-packs",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("endpoint = %q, want %q", test.got, test.want)
			}
		})
	}
}

func TestGroupDMInviteStickerRESTEndpoints(t *testing.T) {
	failedAt := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		token        string
		method       string
		path         string
		requestBody  string
		responseCode int
		responseBody string
		call         func(*Session) error
	}{
		{
			name:         "create group DM",
			token:        "Bearer owner-token",
			method:       http.MethodPost,
			path:         "/api/v10/users/@me/channels",
			requestBody:  `{"access_tokens":["recipient-one","recipient-two"],"nicks":{"111":"one","222":"two"}}`,
			responseCode: http.StatusOK,
			responseBody: `{"id":"group","type":3,"owner_id":"owner","recipients":[{"id":"111"},{"id":"222"}]}`,
			call: func(session *Session) error {
				channel, err := session.GroupDMCreate(&GroupDMCreateParams{
					AccessTokens: []string{"recipient-one", "recipient-two"},
					Nicks: map[string]string{
						"111": "one",
						"222": "two",
					},
				})
				if err == nil && (channel == nil ||
					channel.ID != "group" ||
					channel.Type != ChannelTypeGroupDM ||
					len(channel.Recipients) != 2) {
					t.Fatalf("unexpected group DM response: %#v", channel)
				}
				return err
			},
		},
		{
			name:         "add group DM recipient",
			token:        "Bearer owner-token",
			method:       http.MethodPut,
			path:         "/api/v10/channels/group/recipients/333",
			requestBody:  `{"access_token":"recipient-three","nick":"three"}`,
			responseCode: http.StatusNoContent,
			call: func(session *Session) error {
				return session.GroupDMAddRecipient(
					"group",
					"333",
					&GroupDMAddRecipientParams{
						AccessToken: "recipient-three",
						Nick:        "three",
					},
				)
			},
		},
		{
			name:         "remove group DM recipient",
			token:        "Bearer owner-token",
			method:       http.MethodDelete,
			path:         "/api/v10/channels/group/recipients/333",
			responseCode: http.StatusNoContent,
			call: func(session *Session) error {
				return session.GroupDMRemoveRecipient("group", "333")
			},
		},
		{
			name:         "get invite target users",
			token:        "Bot manager-token",
			method:       http.MethodGet,
			path:         "/api/v10/invites/invite/target-users",
			responseCode: http.StatusOK,
			responseBody: "user_id\n111\n222\n",
			call: func(session *Session) error {
				data, err := session.InviteTargetUsers("invite")
				if err == nil && string(data) != "user_id\n111\n222\n" {
					t.Fatalf("target-users CSV = %q", data)
				}
				return err
			},
		},
		{
			name:         "get invite target users job status",
			token:        "Bot manager-token",
			method:       http.MethodGet,
			path:         "/api/v10/invites/invite/target-users/job-status",
			responseCode: http.StatusOK,
			responseBody: `{"status":3,"total_users":100,"processed_users":41,"created_at":"2025-01-08T12:00:00Z","completed_at":null,"error_message":"Failed to parse CSV file"}`,
			call: func(session *Session) error {
				job, err := session.InviteTargetUsersJobStatus("invite")
				if err == nil && (job == nil ||
					job.Status != InviteTargetUsersJobStatusFailed ||
					job.TotalUsers != 100 ||
					job.ProcessedUsers != 41 ||
					!job.CreatedAt.Equal(failedAt) ||
					job.CompletedAt != nil ||
					job.ErrorMessage == nil ||
					*job.ErrorMessage != "Failed to parse CSV file") {
					t.Fatalf("unexpected target-users job response: %#v", job)
				}
				return err
			},
		},
		{
			name:         "get sticker pack",
			token:        "Bot token",
			method:       http.MethodGet,
			path:         "/api/v10/sticker-packs/847199849233514549",
			responseCode: http.StatusOK,
			responseBody: `{"id":"847199849233514549","stickers":[{"id":"sticker","name":"Wave","format_type":3}],"name":"Wumpus Beyond","sku_id":"847199849233514547","cover_sticker_id":"sticker","description":"Say hello to Wumpus!","banner_asset_id":"banner"}`,
			call: func(session *Session) error {
				pack, err := session.StickerPack("847199849233514549")
				if err == nil && (pack == nil ||
					pack.ID != "847199849233514549" ||
					pack.SKUID != "847199849233514547" ||
					len(pack.Stickers) != 1 ||
					pack.Stickers[0].ID != "sticker") {
					t.Fatalf("unexpected sticker pack response: %#v", pack)
				}
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, err := New(test.token)
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.Method != test.method {
					t.Fatalf("method = %q, want %q", request.Method, test.method)
				}
				if request.URL.Path != test.path {
					t.Fatalf("path = %q, want %q", request.URL.Path, test.path)
				}
				if got := request.Header.Get("Authorization"); got != test.token {
					t.Fatalf("Authorization = %q, want %q", got, test.token)
				}
				body, err := readRequestBody(request)
				if err != nil {
					t.Fatal(err)
				}
				assertJSONEqual(t, body, test.requestBody)
				return jsonResponse(test.responseCode, test.responseBody), nil
			})

			if err := test.call(session); err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestInviteTargetUsersUpdateMultipart(t *testing.T) {
	session, err := New("Bot manager-token")
	if err != nil {
		t.Fatal(err)
	}

	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPut {
			t.Fatalf("method = %q, want %q", request.Method, http.MethodPut)
		}
		if request.URL.Path != "/api/v10/invites/invite/target-users" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bot manager-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if !strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("Content-Type = %q, want multipart/form-data", request.Header.Get("Content-Type"))
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if len(request.MultipartForm.Value) != 0 {
			t.Fatalf("unexpected multipart values: %#v", request.MultipartForm.Value)
		}
		part, header, err := request.FormFile("target_users_file")
		if err != nil {
			t.Fatal(err)
		}
		defer part.Close()
		if header.Filename != "targets.csv" {
			t.Fatalf("filename = %q", header.Filename)
		}
		if got := header.Header.Get("Content-Type"); got != "text/csv" {
			t.Fatalf("file Content-Type = %q, want text/csv", got)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "user_id\n111\n222\n" {
			t.Fatalf("CSV data = %q", data)
		}
		return jsonResponse(http.StatusNoContent, ""), nil
	})

	file := &File{
		Name:   "targets.csv",
		Reader: strings.NewReader("user_id\n111\n222\n"),
	}
	if err := session.InviteTargetUsersUpdate("invite", file); err != nil {
		t.Fatal(err)
	}
	if file.ContentType != "" {
		t.Fatalf("InviteTargetUsersUpdate mutated caller file ContentType to %q", file.ContentType)
	}
}

func TestGroupDMInviteStickerValidation(t *testing.T) {
	tests := []struct {
		name string
		call func(*Session) error
	}{
		{
			name: "nil group DM create parameters",
			call: func(session *Session) error {
				_, err := session.GroupDMCreate(nil)
				return err
			},
		},
		{
			name: "group DM needs multiple users",
			call: func(session *Session) error {
				_, err := session.GroupDMCreate(&GroupDMCreateParams{
					AccessTokens: []string{"one"},
					Nicks:        map[string]string{"111": "one"},
				})
				return err
			},
		},
		{
			name: "blank group DM access token",
			call: func(session *Session) error {
				_, err := session.GroupDMCreate(&GroupDMCreateParams{
					AccessTokens: []string{"one", " "},
					Nicks:        map[string]string{"111": "one", "222": "two"},
				})
				return err
			},
		},
		{
			name: "nil group DM nicks",
			call: func(session *Session) error {
				_, err := session.GroupDMCreate(&GroupDMCreateParams{
					AccessTokens: []string{"one", "two"},
				})
				return err
			},
		},
		{
			name: "blank group DM nick user ID",
			call: func(session *Session) error {
				_, err := session.GroupDMCreate(&GroupDMCreateParams{
					AccessTokens: []string{"one", "two"},
					Nicks:        map[string]string{"": "one"},
				})
				return err
			},
		},
		{
			name: "empty add recipient channel",
			call: func(session *Session) error {
				return session.GroupDMAddRecipient("", "user", &GroupDMAddRecipientParams{AccessToken: "token"})
			},
		},
		{
			name: "empty add recipient user",
			call: func(session *Session) error {
				return session.GroupDMAddRecipient("channel", "", &GroupDMAddRecipientParams{AccessToken: "token"})
			},
		},
		{
			name: "nil add recipient parameters",
			call: func(session *Session) error {
				return session.GroupDMAddRecipient("channel", "user", nil)
			},
		},
		{
			name: "blank add recipient token",
			call: func(session *Session) error {
				return session.GroupDMAddRecipient("channel", "user", &GroupDMAddRecipientParams{})
			},
		},
		{
			name: "empty remove recipient channel",
			call: func(session *Session) error {
				return session.GroupDMRemoveRecipient("", "user")
			},
		},
		{
			name: "empty remove recipient user",
			call: func(session *Session) error {
				return session.GroupDMRemoveRecipient("channel", "")
			},
		},
		{
			name: "empty get target users invite",
			call: func(session *Session) error {
				_, err := session.InviteTargetUsers("")
				return err
			},
		},
		{
			name: "empty update target users invite",
			call: func(session *Session) error {
				return session.InviteTargetUsersUpdate("", &File{Name: "targets.csv", Reader: strings.NewReader("")})
			},
		},
		{
			name: "nil target users file",
			call: func(session *Session) error {
				return session.InviteTargetUsersUpdate("invite", nil)
			},
		},
		{
			name: "empty target users filename",
			call: func(session *Session) error {
				return session.InviteTargetUsersUpdate("invite", &File{Reader: strings.NewReader("")})
			},
		},
		{
			name: "missing target users file source",
			call: func(session *Session) error {
				return session.InviteTargetUsersUpdate("invite", &File{Name: "targets.csv"})
			},
		},
		{
			name: "empty job status invite",
			call: func(session *Session) error {
				_, err := session.InviteTargetUsersJobStatus("")
				return err
			},
		},
		{
			name: "empty sticker pack ID",
			call: func(session *Session) error {
				_, err := session.StickerPack("")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, err := New("Bearer owner-token")
			if err != nil {
				t.Fatal(err)
			}
			session.Client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("validation failure made an HTTP request")
				return nil, nil
			})
			if err := test.call(session); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestInviteTargetUsersJobNullableFields(t *testing.T) {
	var job InviteTargetUsersJob
	err := json.Unmarshal([]byte(`{
		"status":1,
		"total_users":100,
		"processed_users":41,
		"created_at":"2025-01-08T12:00:00Z",
		"completed_at":null,
		"error_message":null
	}`), &job)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != InviteTargetUsersJobStatusProcessing ||
		job.TotalUsers != 100 ||
		job.ProcessedUsers != 41 ||
		job.CompletedAt != nil ||
		job.ErrorMessage != nil {
		t.Fatalf("unexpected target-users job: %#v", job)
	}

	gotStatuses := []InviteTargetUsersJobStatus{
		InviteTargetUsersJobStatusUnspecified,
		InviteTargetUsersJobStatusProcessing,
		InviteTargetUsersJobStatusCompleted,
		InviteTargetUsersJobStatusFailed,
	}
	wantStatuses := []InviteTargetUsersJobStatus{0, 1, 2, 3}
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Fatalf("target-users statuses = %#v, want %#v", gotStatuses, wantStatuses)
	}
}
