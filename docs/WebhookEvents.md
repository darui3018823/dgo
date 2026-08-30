# Webhook Events and Application Identity Profiles

## HTTP Webhook Events

dgo exposes the current Discord Webhook Events envelope without imposing an
HTTP server framework. Verify the request before parsing it, then acknowledge
both PING and ordinary events with an empty 204 response:

```go
func webhookHandler(publicKey ed25519.PublicKey) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !dgo.VerifyWebhookEvent(r, publicKey) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		event, err := dgo.ParseWebhookEventRequest(r)
		if err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if event.Type == dgo.WebhookEventTypePing {
			dgo.AcknowledgeWebhookEvent(w)
			return
		}

		// Decode event.Event.Data according to event.Event.Type here.
		dgo.AcknowledgeWebhookEvent(w)
	})
}
```

`WebhookEvent.Event.Data` remains `json.RawMessage` because its shape depends
on `WebhookEventBody.Type`. Constants for the currently documented event names
are provided as `WebhookEvent...` values. Discord requires the
`X-Signature-Ed25519` and `X-Signature-Timestamp` headers to be validated before
accepting a request. See the
[official Webhook Events documentation](https://docs.discord.com/developers/events/webhook-events)
for endpoint configuration, retry behavior, and event-specific data shapes.

## Application Identity Profiles

The typed `Session` methods cover profile writes and reads, identity lookup by
Discord user or external ID, and identity deletion:

```go
profile, err := session.ApplicationIdentityProfileUpdate(
	applicationID,
	userID,
	providerIssuedUserID,
	&dgo.ApplicationIdentityProfileEdit{Username: &username},
)
```

`ApplicationIdentityProfileUpdate` returns the created profile on Discord's
initial `201 Created` response and returns `nil, nil` for a successful
`204 No Content` update. When `Data` is supplied, Discord replaces the complete
stored data object; send every field that should remain present.

These methods require the bot token for the application and the user's
`application_identities.write` authorization. See the
[official Application Identity Profile documentation](https://docs.discord.com/developers/resources/application-identity-profile)
for limits and authorization details.
