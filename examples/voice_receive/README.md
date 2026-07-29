<img align="right" alt="DiscordGo logo" src="/docs/img/discordgo.svg" width="200">

## dgo Voice Receive Example

This example experiments with receiving voice data from Discord. It joins
a specified voice channel and listens for 10 seconds. By default it does not
persist the received audio. Passing the explicit `-record` flag saves an
unencrypted `.ogg` file for each SSRC found in the channel.

An exercise left to the reader is to translate these SSRCs to user IDs; see
speaking update events for this information.

### Recording safety requirements

Voice recording can expose highly sensitive personal data. Do not use
`-record` unless you have designed and documented a lawful recording process.
At a minimum:

- Obtain explicit, informed consent from every participant before capturing
  or storing their voice. Joining a voice channel is not consent to recording.
- Give participants a clear, visible notice while recording is active and a
  reliable way to decline or stop the recording.
- Define the shortest necessary retention period, delete recordings
  automatically when it expires, and provide a way to honor deletion requests.
- Protect recordings at rest with appropriate encryption and access controls.
  This example writes unencrypted Ogg files and does not provide storage
  encryption.
- Restrict the bot and its operators to the minimum guild and channel
  permissions required. Confirm that the person starting a recording is
  authorized to do so.
- Comply with Discord's terms and policies and all applicable privacy,
  wiretapping, employment, child-safety, and data-protection laws. Requirements
  vary by participant location; obtain qualified legal advice when needed.

If you cannot satisfy these requirements, leave recording disabled. The
default mode receives and discards packets without creating recording files.

This example makes heavy use of the [Pion](https://github.com/pion) family of libraries.
Go check them out for anything to do with voice, video or WebRTC; it's a great
group of people maintaining the project!

Please note that voice receive is **not** officially supported, any may break
at essentially any time (and has in the past). This code works at the time of
its writing, but YMMV in the future.

**Join [Discord Gophers](https://discord.gg/0f1SbxBZjYoCtNPP)
Discord chat channel for support.**

### Build

To build, make sure that modules are enabled, and run:

```sh
go build
```

### Usage

Three flags are required: the bot's token, the guild ID containing the voice
channel to join, and the ID of the voice channel to join. This default command
does not create recording files:

```sh
./voice_receive -t MY_TOKEN -g 1234123412341234 -c 5678567856785678
```

After satisfying all recording safety requirements above, recording can be
enabled explicitly:

```sh
./voice_receive -record -t MY_TOKEN -g 1234123412341234 -c 5678567856785678
```
