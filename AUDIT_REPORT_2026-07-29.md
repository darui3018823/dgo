# dgo 総合監査レポート

- 対象リポジトリ: `github.com/darui3018823/dgo`
- 対象コミット: `1d4f1613163a8029d7ce6368dc687571fcdd52ac`
- 対象ブランチ: `master`
- 監査日: 2026-07-29
- 比較対象:
  - Discord Developer API / Gateway / Voice の2026-07-29時点の公式ドキュメント
  - discord.py 2.7.1
  - upstream `bwmarrin/discordgo`
- 監査方法:
  - ソースコードの静的レビュー
  - 公開API、Gateway event、REST endpoint、modelの仕様照合
  - Discord Developer Terms / Policyとの照合
  - discord.py 2.7.1との機能・安全性比較
  - `go test`、race detector、`go vet`、`govulncheck`、coverage、staticcheck
  - GitHub Actions、release、CodeQL、Dependabot、ドキュメント、nested moduleの確認

## 1. 結論

現状のdgoは、通常のunit test、race detector、`go vet`、CodeQL scheduled scanが成功している一方で、Gateway、Voice、DAVE、rate limit、token logging、現行Discord APIとの型整合性に重大な問題を含んでいる。

特に次の問題は、新規releaseや本番での大規模利用より先に修正する必要がある。

1. Voice eventをgoroutineで並列処理するため、HELLOとREADYの処理順が逆転し、`time.NewTicker(0)`でプロセスがpanicし得る。
2. Gateway OP9 Invalid Sessionのbooleanを無視し、同一接続上で即IDENTIFYする。
3. Gateway fatal close codeを無視して永久再接続する。
4. Gateway送信rate limiterとIdentify concurrency制御がない。
5. bot token、interaction token、webhook token、voice token、voice secret keyをdebug logへ平文出力する。
6. DAVE暗号化・復号がfail-openで、E2EE active中でも平文音声を送受信し得る。
7. Voice UDP/RTP parserに境界検証不足があり、不正packetでpanicし得る。
8. Discordの公開Bot APIに存在しない`POST /invites/{code}`を`InviteAccept`として公開し、self-bot実装を誘発する。
9. Interaction署名検証が未認証request bodyを無制限かつ二重にbufferし、memory DoSを受け得る。
10. Entitlement、Subscription、joined private archived thread等のpagination parameter型が現行APIと異なり、正しく利用できない。

総合判定は以下の通り。

| 観点 | 判定 |
| --- | --- |
| Discord規約・公開API準拠 | 要修正 |
| Main Gatewayの安定性 | 重大な問題あり |
| Voice Gatewayの安定性 | 重大な問題あり |
| DAVE/E2EE | fail-openのため要修正 |
| REST rate limit | 不完全 |
| 現行Discord API coverage | 大きな不足・廃止API残存あり |
| discord.pyとの機能比較 | low-level機能はあるが、Gateway安全性・sharding・高水準APIで大幅不足 |
| 既知脆弱性 | 到達可能な既知脆弱性は0件 |
| test/CI | 基本testは成功。ただし重要領域のcoverageがほぼない |
| release readiness | 現時点では非推奨 |

## 2. 監査上の制限

- 実Discord bot tokenを使用した統合試験は行っていない。
- 実guild、実voice server、実DAVE sessionを利用したend-to-end試験は行っていない。
- Discordの非公開client APIを実際に呼び出す検証は行っていない。
- Discord APIは継続的に変更されるため、本レポートのAPI差分は2026-07-29時点のもの。
- 脆弱性の一部は、network packet、event順序、利用者callback、debug logの設定等、特定条件下で顕在化する。

## 3. Severityの定義

| Severity | 意味 |
| --- | --- |
| Critical | account termination、token reset、E2EE破壊、容易なprocess crash等、直ちに扱うべき問題 |
| High | 本番停止、永久再接続、token漏洩、Discord API revoke、主要機能の恒常的失敗につながる問題 |
| Medium | 特定条件での障害、DoS、誤動作、API互換性低下、危険なdefault |
| Low | deprecated仕様、ログ品質、ドキュメント、保守性等の問題 |

---

# 4. Discord規約・Gateway・Voice準拠

## DGO-DISCORD-001: 非公開Invite Accept APIとself-bot誘発

- Severity: Critical
- 該当箇所:
  - `restapi.go:2331-2341`
  - `discord.go:29-66`
  - `restapi.go:224-226`
  - `oauth2_test.go:12-20`
  - `discord_test.go:15,34-40`

`InviteAccept`は`POST /invites/{code}`を呼び出す。しかし現行の公開Invite APIに存在するのは、GET、DELETE、target-users系であり、invite acceptance用のBot APIは公開されていない。

Botのguild参加はOAuth2 application installationで行う。通常ユーザーをこのrouteで自動参加させる実装は、Discordの非公開client挙動およびself-bot用途に相当する。

さらに`New`とREST request layerは、Bot/Bearer以外のtoken prefixやraw user tokenを明確に拒否しないため、`InviteAccept`と組み合わせて通常ユーザーaccountを自動操作できる形になっている。

影響:

- 利用者のDiscord account termination
- Discord Developer Terms違反の誘発
- 非公開API変更による恒常的な401/403/404
- invalid request limitの消費

推奨:

- `InviteAccept`を削除する。
- 互換性維持が必要ならdeprecated stubとして常に説明付きerrorを返す。
- Bot、Bearer OAuth2、unauthenticated webhookを別constructorまたは別credential型に分ける。
- Gateway接続はBot token以外を拒否する。

根拠:

- [Discord Invite Resource](https://docs.discord.com/developers/resources/invite)
- [Automated User Accounts (Self-Bots)](https://support.discord.com/hc/en-us/articles/115002192352-Automated-User-Accounts-Self-Bots)
- [Discord Developer Terms of Service](https://support-dev.discord.com/hc/en-us/articles/8562894815383-Discord-Developer-Terms-of-Service)

## DGO-DISCORD-002: Voice eventの順序破壊によるpanic

- Severity: Critical
- 該当箇所:
  - `voice.go:462-468`
  - `voice.go:502-510`
  - `voice.go:665-670`
  - `voice.go:695-703`

Voice WebSocket readerはeventごとに`go v.onEvent(...)`を起動する。このため、wire上ではHELLO、READYの順に到着しても、READY handlerが先に実行され得る。

READYが先に処理されると、HeartbeatIntervalが0のままheartbeat goroutineへ渡され、`time.NewTicker(0)`がruntime panicを発生させる。

DAVE prepare/execute/commit等のstateful eventも同様に並べ替わるため、E2EE state machineの破損原因になる。

推奨:

- Voice Gateway eventは単一ordered loopで逐次処理する。
- 利用者callback等、順序非依存の処理だけを別goroutineへ渡す。
- HELLOを受け取るまでREADY処理を進めないstate machineとする。
- heartbeat intervalが0以下なら明示的errorで接続を閉じる。

根拠:

- [Discord Voice Connections](https://docs.discord.com/developers/topics/voice-connections)

## DGO-DISCORD-003: Gateway OP9 Invalid Sessionの処理が仕様と逆

- Severity: High
- 該当箇所: `wsapi.go:624-636`

OP9の`d`はbooleanであり、resume可能性を示す。しかし実装は`d`をdecodeせず、常に同一接続上へ即IDENTIFYを送る。

正しい動作:

- `d=true`: 接続し直してRESUMEを試みる。
- `d=false`: 接続を閉じ、session IDとsequenceを破棄し、新しい接続でIDENTIFYする。

現実装では、既にauthenticatedなconnectionへ二重IDENTIFYし、4005、再接続loop、IDENTIFY quota消費を引き起こし得る。

DiscordはIDENTIFYを24時間あたり1000回に制限しており、超過時は全session終了とbot token resetを行う。

推奨:

- OP9 payloadをbooleanとしてdecodeする。
- connectionを閉じてからresume/fresh identifyを選択する。
- fresh identify時はsession ID、sequence、resume URLを原子的にclearする。
- random backoffとsession start limit管理を追加する。

根拠:

- [Gateway Resuming](https://docs.discord.com/developers/events/gateway#resuming)
- [Gateway Identifying](https://docs.discord.com/developers/events/gateway#identifying)

## DGO-DISCORD-004: Gateway fatal close codeでも永久再接続する

- Severity: High
- 該当箇所:
  - `wsapi.go:224-247`
  - `wsapi.go:886-938`

Gateway readerはWebSocket close codeを分類せず、全errorに対して`Close()`と`reconnect()`を実行する。

4004、4010、4011、4012、4013、4014等は公式にReconnect=falseである。不正token、不正sharding、invalid intents、disallowed intentsで永久再接続し続ける。

4007/4009では古いsessionを捨てる必要があるが、session stateをclearしないため不正RESUMEを反復し得る。

影響:

- 不要なnetwork traffic
- Discord側のinvalid request/Identify枠消費
- botが永久に起動完了しない
- error logの大量出力
- account/application enforcementリスク

推奨:

- close codeごとに`stop`、`resume`、`fresh identify`を分類するtableを持つ。
- terminal error channelまたはcallbackを公開する。
- fatal close codeでは自動再接続を停止する。

根拠:

- [Gateway Close Event Codes](https://docs.discord.com/developers/topics/opcodes-and-status-codes)

## DGO-DISCORD-005: HELLO後、READYまでheartbeatを開始しない

- Severity: High
- 該当箇所:
  - `wsapi.go:110-130`
  - `wsapi.go:178-210`
  - `wsapi.go:286-328`

`OpenWithContext`はHELLO受信後にIDENTIFYを送り、次の1 packetとしてREADY/RESUMEDを同期的に読む。その後で初めてheartbeatとlistenerを開始する。

Discord仕様ではHELLO受信後、READY待ち中もheartbeatを開始する必要がある。Discord障害やlarge guild初期化等でREADYが遅れると、heartbeatを一度も送らず切断される。

加えて、heartbeat goroutineは起動直後に送信しており、最初のheartbeatに`heartbeat_interval * random jitter`を適用していない。

追加のdeadlock経路:

- `OpenWithContext`はSession write lockを保持する。
- READY待ちのpacketがOP7等であれば、`onEvent`から`CloseWithCode`が同じSession lockを取ろうとして自己deadlockする。

推奨:

- HELLO直後にlistenerとheartbeat state machineを開始する。
- READY待ちはchannel/stateで待つ。
- 初回heartbeatへ0から1のjitterを適用する。
- `OpenWithContext`中に長時間Session write lockを保持しない。

根拠:

- [Gateway Heartbeat Interval](https://docs.discord.com/developers/events/gateway#heartbeat-interval)

## DGO-DISCORD-006: Heartbeat ACK欠落を5周期放置する

- Severity: High
- 該当箇所:
  - `wsapi.go:273-274`
  - `wsapi.go:298-316`

`FailedHeartbeatAcks = 5`により、ACKがないzombie connectionを最大5 interval維持する。

Discord仕様は、次のheartbeatを送る時点で前回ACKがなければ直ちに切断し、RESUMEすることを求める。

推奨:

- `awaitingAck` flagをheartbeatごとに管理する。
- 次回送信前に未ACKなら、1000/1001以外でconnectionを切断しRESUMEする。
- latencyとmissed ACK countをmetricとして公開する。

## DGO-DISCORD-007: resume_gateway_urlを保持しない

- Severity: High
- 該当箇所:
  - `events.go:37-46`
  - `event.go:243-248`
  - `wsapi.go:71-80`

`Ready`に`resume_gateway_url` fieldがなく、`onReady`はSessionIDだけを保存する。再接続は最初に取得したGateway URLを使う。

Discordは、Resume時に`resume_gateway_url`を使用しない場合、通常より切断率が高くなると明記している。

推奨:

- `Ready.ResumeGatewayURL`を追加する。
- initial identify URLとresume URLを別に保持する。
- session invalidation時にresume URLをclearする。

## DGO-DISCORD-008: Gateway送信rate limiterがない

- Severity: High
- 該当箇所:
  - `wsapi.go:420-444`
  - `wsapi.go:526-538`
  - `wsapi.go:541-552`
  - `wsapi.go:759-774`

Gateway writeは`wsMutex`によって同時writeだけを防止しているが、120 events / 60 secondsの送信limitを管理しない。

`UpdateStatusComplex`、`GatewayWriteStruct`、Request Guild Members、voice state update等を連打すると4008で切断される。反復違反はAPI access revoke対象になる。

推奨:

- すべてのMain Gateway writeを単一rate-aware queueへ集約する。
- heartbeat、IDENTIFY、RESUMEを含むopcodeごとの会計を仕様に合わせる。
- discord.pyと同様にlimitより少し保守的な内部上限を使用する。

根拠:

- [Gateway Rate Limiting](https://docs.discord.com/developers/events/gateway#rate-limiting)

## DGO-DISCORD-009: Identify concurrencyとshard orchestrationがない

- Severity: High
- 該当箇所:
  - `structs.go:63-65`
  - `wsapi.go:848-883`

dgoはShardID/ShardCountを単一Sessionに載せる機能は持つが、`Get Gateway Bot`が返す`session_start_limit.max_concurrency`を使ったshard identify調停を持たない。

複数Sessionを同時に`Open`すると、concurrent identify/5秒制限へ抵触し、OP9、切断、IDENTIFY quota消費につながる。

推奨:

- ShardManagerを追加する。
- `max_concurrency` bucketごとの5秒windowを管理する。
- total、remaining、reset_afterを利用者へ公開する。
- Identifyの前にhookまたはschedulerを通す。

## DGO-DISCORD-010: Request Guild Membersのpayloadとrate limitが現行仕様と異なる

- Severity: High
- 該当箇所:
  - `wsapi.go:447-455`
  - `wsapi.go:469-523`

`guild_id`を`[]string`としてserializeし、通常の単一guild requestでも1要素arrayを送信する。現行仕様は単一snowflakeである。

deprecated batch APIは複数guildを1 requestに含めるが、現行仕様は1 requestにつき1 guild。

さらに以下が未対応。

- `presences=true`に必要な`GUILD_PRESENCES` intentの事前検証
- 全member requestに必要な`GUILD_MEMBERS` intentの事前検証
- guild/botごとの30秒rate limit
- Discordから返されるGateway `RATE_LIMITED` event

推奨:

- `GuildID string`へ変更する。
- batch APIを削除またはdeprecatedにする。
- intent、nonce長、query/user_ids排他を検証する。
- guild/bot単位の30秒limiterを追加する。
- typed `RateLimited` eventを追加する。

根拠:

- [Request Guild Members](https://docs.discord.com/developers/events/gateway-events)
- [Discord Developer Change Log](https://docs.discord.com/developers/change-log)

## DGO-DISCORD-011: Voice Gateway v8 opcode/ACK処理が不正確

- Severity: High
- 該当箇所:
  - `voice.go:522-525`
  - `voice.go:626`
  - `voice.go:665-713`

問題:

- OP3をheartbeat responseとして扱うが、Voice v8 Heartbeat ACKはOP6。
- OP6を処理しない。
- heartbeat ACKを記録・監視しない。
- Clients ConnectをOP12として扱うが、現行opcodeは11。
- heartbeat nonceが`Unix()`秒単位で、ミリ秒精度を持たない。

影響:

- zombie voice connectionを検出できない。
- voice client list eventを取り落とす。
- latency測定不能。
- Discord Voice protocolとのinterop低下。

推奨:

- OP6 ACK handlerを実装する。
- OP11 Clients Connectを実装する。
- Voice RTTとACK timeoutを管理する。
- heartbeat nonceを`UnixMilli()`等へ変更する。

## DGO-DISCORD-012: Voice close codeの分類が不足

- Severity: High
- 該当箇所: `voice.go:407-457`

4014/4017以外を原則再接続するが、4021 rate limitedと4022 call terminatedは公式にShould not reconnectである。

推奨:

- Voice close code tableを実装する。
- 4014、4015等のresume/reconnect条件を現行仕様へ合わせる。
- 4021/4022等はterminalとして停止する。

## DGO-DISCORD-013: 通常close codeでResume用sessionを無効化し得る

- Severity: Medium
- 該当箇所:
  - `wsapi.go:240`
  - `wsapi.go:314`
  - `wsapi.go:944-985`

network errorやheartbeat failure時にも`Close()`がnormal closure 1000を送信する。Discord仕様では1000/1001でsessionが無効化されるが、その後のreconnectは既存SessionID/sequenceを使ってRESUMEしようとする。

推奨:

- 利用者による正常終了と、RESUME目的の異常切断を分ける。
- RESUME目的では1000/1001を使用しない。
- close reasonをstate machineのenumで明示する。

## DGO-DISCORD-014: token、secret key、API Dataの平文logging

- Severity: High
- 該当箇所:
  - `wsapi.go:598`
  - `wsapi.go:877-878`
  - `restapi.go:211-214`
  - `restapi.go:321-322`
  - `endpoints.go:160-162`
  - `endpoints.go:225-238`
  - `voice.go:363`
  - `voice.go:486`
  - `voice.go:973`
  - `voice.go:1020`

漏洩対象:

- `Identify.Token`
- `INTERACTION_CREATE.token`
- `VOICE_SERVER_UPDATE.token`
- webhook tokenを含むURL
- interaction tokenを含むURL
- VoiceConnection struct内のvoice token/session data
- Voice OP4 `secret_key`
- message content等のAPI Data

Authorization headerは`restapi.go:243-256`でredactされるが、URL path、payload、Gateway raw dataはredactされない。

影響:

- application/bot takeover
- webhook takeover
- interaction callbackの不正利用
- voice session compromise
- log storageを通じたAPI Data漏洩
- Discord Developer Termsのcredential保護要件違反

推奨:

- logger直前に共通secret redactorを置く。
- webhook/interaction URLをroute templateへ変換する。
- Identify/Voice payloadを構造化logに変更し、tokenとkeyを常に除外する。
- raw payload loggingはdefault無効とし、明示opt-inでもsecret fieldを除外する。
- `RateLimit.URL`にはsanitized routeのみを入れる。

根拠:

- [Discord Developer Terms §2 Developer Credentials](https://support-dev.discord.com/hc/en-us/articles/8562894815383-Discord-Developer-Terms-of-Service)

## DGO-DISCORD-015: REST global rate limit待機がroute waitに負ける

- Severity: High
- 該当箇所: `ratelimit.go:82-96`

route bucketのwaitが存在すると即returnするため、より長いglobal waitを評価しない。

例:

- route wait: 1秒
- global wait: 5秒
- 実装結果: 1秒後にrequestを送信

また待機中に別goroutineがglobal limitを設定しても、起床後に条件を再評価しない。

推奨:

- route/global waitの最大値を使う。
- sleep後に条件をloopで再評価する。
- session全体のproactive 50 requests/second token bucketを追加する。

根拠:

- [HTTP Rate Limits](https://docs.discord.com/developers/topics/rate-limits)

## DGO-DISCORD-016: Reaction用hard-coded limiterがDiscord headerを無視する

- Severity: High
- 該当箇所:
  - `ratelimit.go:37-43`
  - `ratelimit.go:156-166`

reaction requestを1回/200msに固定し、custom bucketの場合はDiscordから返る`X-RateLimit-*`、global、bucket hashを処理せずreturnする。

Discordはrate limit値をhard-codeせずresponse headerに従うよう要求している。

影響:

- global 429が他bucketへ伝播しない。
- Discord側の変更に追随できない。
- 不要な過剰throttleまたは429。

推奨:

- custom reaction limiterを削除する。
- 前段の保守的limiterとして残す場合も、必ずDiscord headerを処理する。

## DGO-DISCORD-017: Allowed Mentionsの危険なdefault

- Severity: Medium
- 該当箇所:
  - `message.go:245-255`
  - `interactions.go:603`
  - `examples/echo/main.go:30-43`

通常messageで`allowed_mentions`を省略すると、users、roles、everyoneがparseされる。dgoはsession-levelの安全なdefaultを持たず、user inputをそのまま送るhelperやexampleがunexpected pingを発生させ得る。

discord.pyは`Client.allowed_mentions`を持ち、global defaultとmessage単位設定をmergeできる。

推奨:

- Sessionへdefault allowed mentionsを追加する。
- 安全側のdefaultを`parse: []`とする。
- echo等のexampleでは明示的にmentionを抑止する。
- `parse`と`users`/`roles`の相互排他、最大100件を送信前検証する。

根拠:

- [Discord Allowed Mentions](https://docs.discord.com/developers/resources/message#allowed-mentions-object)

## DGO-DISCORD-018: Default intentsがdata minimizationと逆方向

- Severity: Medium
- 該当箇所:
  - `discord.go:64`
  - `structs.go:3028-3092`

新規Sessionは`IntentsAllWithoutPrivileged`をdefaultとし、typing、invites、webhooks、integrations、automod等を用途に関係なく要求する。

Discord Developer Policyは、applicationのstated functionalityに必要なAPI Dataだけへaccessすることを求める。

さらにpoll intentsは定義済みなのに`IntentsAllWithoutPrivileged`と`IntentsAll`に含まれず、名前と実値が一致しない。

推奨:

- defaultを`IntentsNone`または小さい明示presetへ変更する。
- 用途別presetを追加する。
- poll intent bitsをaggregateへ追加する。
- intentと必要event/cacheの対応をdocument化する。

## DGO-DISCORD-019: Voice録音exampleのprivacy/security注意不足

- Severity: Medium
- 該当箇所:
  - `examples/voice_receive/main.go:42-66`
  - `examples/voice_receive/README.md`

受信音声をSSRCごとの`.ogg`へ無条件かつ平文保存するが、以下の注意がない。

- 録音への明示同意
- 参加者への可視通知
- retention期間
- 削除手段
- 保存時暗号化
- guild/channel権限と法的要件

推奨:

- example名とREADMEで録音を明示する。
- 同意、通知、retention、削除、at-rest encryption要件を警告する。
- defaultではdiskへ保存しないexampleを検討する。

## DGO-DISCORD-020: Stage Instance event名が誤っている

- Severity: Medium
- 該当箇所:
  - `eventhandlers.go:66-68`
  - `tools/cmd/eventhandlers/main.go:99-119`

登録名は以下。

- `STAGE_INSTANCE_EVENT_CREATE`
- `STAGE_INSTANCE_EVENT_UPDATE`
- `STAGE_INSTANCE_EVENT_DELETE`

公式wire event名は以下。

- `STAGE_INSTANCE_CREATE`
- `STAGE_INSTANCE_UPDATE`
- `STAGE_INSTANCE_DELETE`

そのためtyped handlerが発火しない。

推奨:

- wire名を明示metadataとしてgeneratorへ渡す。
- またはGo typeを`StageInstanceCreate/Update/Delete`へ変更する。
- event名のgolden testを追加する。

## DGO-DISCORD-021: Identify connection propertiesがdeprecated形式

- Severity: Low
- 該当箇所: `structs.go:2350-2355`

`$os`、`$browser`、`$device`を使用する。現在も動作するが、公式は`os`、`browser`、`device`を現行形式としている。

推奨:

- 現行field名へ移行する。
- legacy `$` fieldが必要ならcompatibility optionにする。

## DGO-DISCORD-022: Interaction followupの契約が不正確

- Severity: Low
- 該当箇所:
  - `restapi.go:2589-2591`
  - `restapi.go:3330-3331`
  - `examples/slash_commands/main.go:496`

followup APIは`wait bool`を受け、falseならMessageを捨てる。しかしinteraction followupでは`wait`は常にtrueとして扱われる。

またuser-installedかつguild未install interactionではfollowup最大5件という制限があるが、exampleは無制限に作れると説明する。

推奨:

- followup helperから`wait`引数を除去する。
- 常にMessageをdecodeする。
- 5件制限をdocumentationへ反映する。

---

# 5. Security・Reliability・Concurrency

## DGO-SEC-001: Voice WebSocket readerが引数ではなく可変current connectionを読む

- Severity: High
- 該当箇所: `voice.go:400-405`

`wsListen(wsConn, close)`はconnectionを引数で受け取るが、実際には`v.wsConn.ReadMessage()`を呼ぶ。

再接続で`v.wsConn`が新connectionへ差し替わると、旧listenerと新listenerが同じ新connectionを同時に読む。gorilla/websocketは1 connectionにつき1 readerを要求する。

影響:

- event消失
- frame破損
- data race
- reconnect loop
- DAVE/Voice stateの不整合

推奨:

- 必ず引数`wsConn.ReadMessage()`を使用する。
- connection generation IDを持ち、旧listenerを確実にcancel/waitする。

## DGO-SEC-002: Voice UDP/RTP parserのbounds check不足

- Severity: High
- 該当箇所: `voice.go:1034-1072`

`rlen >= 12`だけを確認した後、以下を十分なlength検証なしでsliceする。

- CSRC count由来の`12 + 4*i`
- `rlen - 4`
- `plainLength`
- RTP extension length
- AEAD overhead

networkから短い、malformed、truncated packetが届くとslice bounds panicでprocessが終了する。

推奨:

- RTP fixed header、CSRC、extension header、extension payload、nonce、AEAD tagの順に段階的にlengthを検証する。
- invalid packetはdropしmetricを増やす。
- fuzz testを追加する。

## DGO-SEC-003: DAVE送信がfail-open

- Severity: High
- 該当箇所:
  - `voice.go:928-951`
  - `dave.go:364-368`

DAVE `EncryptFrame`が失敗した場合、errorをlogするだけで元Opusをtransport AEADへ載せて送信する。

DAVE active sessionではE2EE暗号化失敗時にframeをdropすべきであり、平文fallbackはE2EE保証を破る。

推奨:

- DAVE active中のencrypt errorはframeをdropする。
- error countがthresholdを超えたらvoice connectionを閉じる。
- fail-openを許す場合は明示的unsafe optionに限定する。

## DGO-SEC-004: DAVE受信がfail-open

- Severity: High
- 該当箇所:
  - `dave.go:262-275`
  - `dave.go:113-116`
  - `voice.go:1187-1189`

`parseSecureFrame`が`errNotDAVEFrame`を返すと、DAVE activeかどうかを確認せず元dataを平文として返す。

また以下が不完全。

- `active` fieldを受信判定に利用しない。
- HandleCommitがno-op。
- proposalsを無視する。

推奨:

- DAVE active中は非DAVE frameをdropする。
- protocol state transitionを完全に実装する。
- prepare/execute/commit/proposalsのinterop testを追加する。
- replay/generation/nonce testを追加する。

## DGO-SEC-005: DAVE secure frame parserがULEB128妥当性を検証しない

- Severity: Medium
- 該当箇所: `dave_crypto.go:67-77,80-106`

`decodeULEB128`はoverflow、最大byte数、terminating byteをerrorとして返さず、`parseSecureFrame`もdecode結果のconsumed lengthを無視する。

tag検証により多くは拒否されると考えられるが、malformed packet処理として不十分。

推奨:

- decode関数を`(value, consumed, error)`へ変更する。
- uint32最大5 byte、termination、overflow、trailing byteを検証する。

## DGO-SEC-006: Session.CloseがVoice connectionを閉じない

- Severity: High
- 該当箇所: `wsapi.go:942-965`

コメント上もTODOのままで、Main Gatewayを閉じてもVoice WebSocket、UDP、heartbeat、opus、reconnect goroutineが残り得る。

`voice.go:1124-1159`のreconnect loopにもcancel条件がなく、Sessionがreadyにならない場合は永久に残る可能性がある。

推奨:

- Close時にVoiceConnectionsをsnapshotし、全connectionをcloseする。
- context/cancelとWaitGroupを導入する。
- Closeをidempotentにする。
- goroutine leak testを追加する。

## DGO-SEC-007: Interaction署名検証が未認証bodyを無制限に二重bufferする

- Severity: High
- 該当箇所: `interactions.go:620-661`

署名検証前にrequest body全体を以下へcopyする。

- signature verification用`msg`
- body復元用`body`

外部から任意サイズの署名不正requestを送るだけで、bodyのおよそ2倍以上をallocateさせられる。

推奨:

- `http.MaxBytesReader`または`io.LimitedReader`を使用する。
- Content-Length上限を検証する。
- bodyを1回だけbounded readし、timestampとのverify用bufferを最小化する。
- stale timestamp/replayを必要に応じて拒否する。

## DGO-SEC-008: REST 429 retryが無制限再帰

- Severity: Medium-High
- 該当箇所: `restapi.go:312-337`

429ごとに同じsequenceで`RequestWithLockedBucket`を再帰呼び出しする。502 retryと異なり上限がない。

各stack frameの`resp.Body.Close()`は再帰がunwindするまでdeferされる。永続429でstack、goroutine、response resourceが増加する。

推奨:

- iterative loopへ変更する。
- max retry、max total wait、max ratelimit timeoutを設定可能にする。
- jitterとcontext cancellationを維持する。

## DGO-SEC-009: RequestRawが不正URL/methodでpanicする

- Severity: Medium
- 該当箇所: `restapi.go:188-206`

context抽出用の`http.NewRequest` errorを無視した後、`cfg.Request.Context()`を呼ぶ。不正URL等でRequestがnilならpanicする。

推奨:

- 最初の`http.NewRequest` errorを返す。
- public `RequestWithLockedBucket`ではnil bucketも検証する。
- optionの二重実行を避ける。

## DGO-SEC-010: HTTP成功codeとretry対象が狭すぎる

- Severity: Medium
- 該当箇所: `restapi.go:299-310`

200、201、204だけを成功扱いし、その他の正当な2xxをRESTErrorにする。

transient retryは502のみで、500、504、524、connection reset等を即失敗させる。

discord.pyは全2xxを成功扱いし、transient errorをbounded loopでretryする。

推奨:

- `200 <= status < 300`を成功扱いする。
- 500/502/504/524と一部network errorをbounded exponential backoffでretryする。
- idempotencyを考慮し、unsafe methodのretry policyを分ける。

## DGO-SEC-011: RateLimiter mapが無期限成長する

- Severity: Medium
- 該当箇所: `ratelimit.go:21-79`

`buckets`と`bucketHashes`にevictionがない。多数のguild、channel、webhook tokenを扱う長寿命processではmapが増え続ける。

webhook tokenを含むbucket keyを長期間memoryへ保持する点もcredential lifetimeを延ばす。

推奨:

- last-used timestampを持つLRU/TTL evictionを追加する。
- route keyをsanitized major parameter keyへ変換する。
- tokenを直接map keyへ保持しない。

## DGO-SEC-012: X-RateLimit-Bucket mappingがmajor parameterを失う

- Severity: Medium
- 該当箇所: `ratelimit.go:172-186`

Discordのbucket hashはtop-level resourceを含まないが、実装はhashだけでbucketを共有する。

異なるchannel/guild/webhook major parameterのrouteが同一bucketへ過剰統合され、不要なthrottleを起こす。

推奨:

- `bucket hash + major parameters`を内部bucket keyにする。
- HTTP methodとnormalized routeも仕様に合わせて扱う。

## DGO-SEC-013: OpenWithContextがDial後にcancel不能

- Severity: Medium
- 該当箇所:
  - `wsapi.go:55-64`
  - `wsapi.go:71-130`
  - `wsapi.go:178-185`

contextはWebSocket DialContextにしか使われず、Gateway REST requestやHELLO/READYの同期`ReadMessage`にdeadline/cancel watcherがない。

Session write lockを保持したまま無期限blockし得る。

推奨:

- Gateway RESTへ`WithContext(ctx)`を渡す。
- handshake read deadlineを設定する。
- context cancel時にconnectionをcloseするwatcherを起動する。
- write lockのscopeを短縮する。

## DGO-SEC-014: 利用者event handlerのpanicがprocessを終了させる

- Severity: Medium
- 該当箇所:
  - `event.go:167-183`
  - `voice.go:622-624`

非同期handlerは裸のgoroutineで実行される。Goでは任意goroutineの未recover panicがprocess全体を終了させる。

discord.pyはevent exceptionをerror hookへ配送する。

推奨:

- handler wrapperで`recover`する。
- `PanicHandler`またはevent error hookを提供する。
- stack traceをsecret redaction後に記録する。
- fail-fastを希望する利用者向けoptionを分ける。

## DGO-SEC-015: SyncEvents handlerがhandler lock下で実行されdeadlockする

- Severity: High
- 該当箇所: `event.go:167-198`

`handleEvent`は`handlersMu.RLock`を保持したまま利用者handlerを実行する。

`SyncEvents=true`でhandler内からAddHandler、remove callback、AddHandlerOnceを呼ぶとwrite lock取得待ちでdeadlockする。

また`onceHandlers[t] = nil`をRLock下で変更しており、並行event dispatch時のrace要因になる。

推奨:

- lock下でhandler sliceをcopyする。
- lockを解放してからcallbackを実行する。
- once handlerの取り出しと削除はwrite lock下で原子的に行う。
- deadlock/race regression testを追加する。

## DGO-SEC-016: Voice public APIにTOCTOU、nil panic、data race経路がある

- Severity: Medium
- 該当箇所:
  - `voice.go:101-108`
  - `voice.go:123-140`
  - `wsapi.go:712-724`
  - `wsapi.go:759-774`

例:

- Speakingはnil checkとWriteJSONが同一lockで保護されず、Closeと競合する。
- ChangeChannelはsession WebSocket nil guardが不統一。
- VoiceConnection fieldの更新が一貫してlockされない。
- `VoiceConnections` mapがnilのまま代入され得る。
- closed Sessionでmanual voice joinするとnil pointer経路がある。

推奨:

- public voice methodに一貫したstate validationを追加する。
- connection pointerをlock下でsnapshotする。
- `ErrWSNotFound`等のtyped errorを返す。
- race testを追加する。

## DGO-SEC-017: Multipart helperが添付全体をmemoryへbufferする

- Severity: Medium
- 該当箇所:
  - `util.go:28-76`
  - `util.go:81-117`

multipart requestは全fileを`bytes.Buffer`へ読み込む。大容量添付や複数fileでOOMになり得る。retry中も巨大`[]byte`を保持する。

推奨:

- `io.Pipe`を利用したstreaming multipartを検討する。
- retry可能性が必要ならseekable/reopenable reader contractを用意する。
- request sizeを事前検証する。

## DGO-SEC-018: zlib error logが実際のerrorを出さない

- Severity: Low
- 該当箇所: `wsapi.go:574-583`

zlib open/close error時に、`err2`/`err3`ではなく外側の`err`をlogするため、`<nil>`等の無意味なerrorになる。

推奨:

- 正しいlocal errorをlogする。
- compressed Gateway payloadのmalformed testを追加する。

## DGO-SEC-019: ErrUnauthorizedがRESTErrorに上書きされる

- Severity: Low
- 該当箇所: `restapi.go:341-348`

401で`ErrUnauthorized`を設定した後にfallthroughし、`newRestError`で上書きする。

推奨:

- `errors.Is`可能なwrap構造にする。
- token prefix guidanceはtyped error metadataへ移す。

## DGO-SEC-020: GuildEditがVoiceRegions errorを無視する

- Severity: Low
- 該当箇所: `restapi.go:694-708`

VoiceRegions取得errorを無視し、regionsがnilの場合に正当なregionを「Region not valid」と誤拒否する。

推奨:

- validation用network request自体を削除するか、取得errorを返す。
- 現行APIでregion parameterが必要か再確認し、廃止済みならfieldをdeprecated化する。

---

# 6. 現行Discord REST / Modelとの差分

## DGO-API-001: Entitlement pagination parameterの型が誤っている

- Severity: High
- 該当箇所:
  - `structs.go:2633-2640`
  - `restapi.go:3757-3760`

`EntitlementFilterOptions.Before/After`が`*time.Time`で、RFC3339を送る。現行APIはentitlement snowflake IDを要求する。

さらに`exclude_deleted` optionがない。

推奨:

- before/afterをsnowflake IDまたはSnowflake interfaceへ変更する。
- `exclude_deleted`を追加する。
- pagination request testを追加する。

根拠:

- [Entitlement Resource](https://docs.discord.com/developers/resources/entitlement)

## DGO-API-002: Subscription pagination parameterの型が誤っている

- Severity: High
- 該当箇所: `restapi.go:3810-3818`

Subscriptionのbefore/afterにも`*time.Time`を使いRFC3339を送るが、現行APIはsubscription snowflake IDを要求する。

推奨:

- snowflake IDへ変更する。
- pagination responseとhas-more behaviorをtestする。

根拠:

- [Subscription Resource](https://docs.discord.com/developers/resources/subscription)

## DGO-API-003: Application IntegrationTypesConfigのJSON tagが誤っている

- Severity: High
- 該当箇所: `structs.go:181`

`IntegrationTypesConfig`のtagが`json:"integration_types,omitempty"`だが、正しいfieldは`integration_types_config`。

影響:

- READY/GET Applicationで設定を取り落とす。
- request送信時に別fieldとして送る。

推奨:

- JSON tagを修正する。
- fixture-based marshal/unmarshal testを追加する。

## DGO-API-004: 新Modal component未対応

- Severity: High
- 該当箇所:
  - `components.go:13-75`
  - `interactions.go:417`

不足component:

- Label: type 18
- File Upload: type 19
- Radio Group: type 21
- Checkbox Group: type 22
- Checkbox: type 23

unknown component typeをerrorにするため、これらを含むModal Submit Interaction全体がdecode失敗する。

推奨:

- model、marshal/unmarshal、constructor、validationを追加する。
- unknown componentをraw representationとして保持するforward-compatible fallbackを検討する。

根拠:

- [Discord Component Reference](https://docs.discord.com/developers/components/reference)

## DGO-API-005: Get Entitlementが欠落し、test create responseを捨てる

- Severity: Medium-High
- 該当箇所: `restapi.go:3790`

- Get Entitlement endpoint helperがない。
- `EntitlementTestCreate`はDiscordが返すpartial entitlementを捨て、errorしか返さない。

推奨:

- `Entitlement(applicationID, entitlementID)`を追加する。
- create helperは`(*Entitlement, error)`を返す。

## DGO-API-006: Joined private archived threadsのbefore型が誤っている

- Severity: High
- 該当箇所: `restapi.go:3081-3085`

public/private archived threadsではtimestampが正しいが、joined private archived threadsの`before`はthread snowflake IDである。現実装はRFC3339 timestampを送る。

推奨:

- endpointごとにpagination typeを分ける。

## DGO-API-007: 現行Paginated Pins API未対応

- Severity: High
- 該当箇所:
  - `endpoints.go:143-144`
  - `restapi.go:2114-2138`

旧route`/channels/{id}/pins`のみを使用する。

現行route:

- `/channels/{id}/messages/pins`
- `/channels/{id}/messages/pins/{message.id}`

GET responseの`items`、`has_more`、before、limitにも未対応。

推奨:

- 新routeとresponse modelを追加する。
- 旧routeをdeprecated化する。

## DGO-API-008: 現行Permission定数が不足

- Severity: High
- 該当箇所:
  - `structs.go:2700-2706`
  - `structs.go:2822-2850`

不足:

- `SET_VOICE_CHANNEL_STATUS` `1 << 48`
- `PIN_MESSAGES` `1 << 51`
- `BYPASS_SLOWMODE` `1 << 52`

`PermissionAll*` aggregateも現行permissionを十分含まない。

特にPIN_MESSAGESは2026-02-23以降、MANAGE_MESSAGESで代用できない。

根拠:

- [Discord Permissions](https://docs.discord.com/developers/topics/permissions)
- [Discord Change Log](https://docs.discord.com/developers/change-log)

## DGO-API-009: Soundboard機能がほぼ全欠落

- Severity: High

不足:

- SoundboardSound model
- default sound list
- guild soundboard list/create/edit/delete
- send soundboard sound
- Gateway OP31 Request Soundboard Sounds
- Soundboard/Guild Soundboard event
- Voice Channel Effect event

feature/permission定数の一部だけ存在する。

推奨:

- Discord公式Soundboard resourceを1 feature groupとして実装する。
- REST、Gateway event、state cache、permission testをまとめて追加する。

根拠:

- [Soundboard Resource](https://docs.discord.com/developers/resources/soundboard)

## DGO-API-010: Voice Channel Infoの半対応

- Severity: High
- 該当箇所:
  - `events.go:359-373`

VoiceChannelStatusUpdate/StartTimeUpdate受信型はあるが、以下がない。

- `PUT /channels/{id}/voice-status`
- Request Channel Info OP43
- Channel Info response model
- `SET_VOICE_CHANNEL_STATUS` permission

そのためstatus設定と初期取得ができない。

## DGO-API-011: Gateway RATE_LIMITED eventがない

- Severity: High

Request Guild Members等のGateway operationがrate limitedになってもtyped eventを受信できず、unknown event warningとraw Eventだけになる。

REST synthetic `RateLimit` eventとは別物である。

## DGO-API-012: Primary Entry Point / Launch Activity未対応

- Severity: High
- 該当箇所:
  - `interactions.go:21-30`
  - `interactions.go:33-55`
  - `interactions.go:576-588`

不足:

- Application Command type 4 `PRIMARY_ENTRY_POINT`
- ApplicationCommand `handler`
- Interaction response type 12 `LAUNCH_ACTIVITY`
- Get Application Activity Instance

## DGO-API-013: Search Guild Messages未対応

- Severity: Medium-High

2026-03追加のSearch Guild Messages endpoint、filter、result modelがない。

根拠:

- [Message Resource](https://docs.discord.com/developers/resources/message)

## DGO-API-014: 廃止済みGuild作成APIが残る

- Severity: Medium
- 該当箇所:
  - `restapi.go:664`
  - `restapi.go:1694`

`GuildCreate`と`GuildCreateWithTemplate`はapps向けには廃止され、OpenAPIから削除済み。

推奨:

- deprecated化後にmajor releaseで削除する。

## DGO-API-015: 廃止済みchannel active threads routeが残る

- Severity: Medium
- 該当箇所: `restapi.go:3000`

`/channels/{id}/threads/active`はAPI v10で廃止済み。現行はguild active threadsを使用する。

## DGO-API-016: 廃止・無効化済みintegration/permissions APIが残る

- Severity: Medium
- 該当箇所:
  - `restapi.go:1297`
  - `restapi.go:1315`
  - `restapi.go:3276`

対象:

- GuildIntegrationCreate
- GuildIntegrationEdit
- ApplicationCommandPermissionsBatchEdit

現行公開APIでは削除またはdisabled。

## DGO-API-017: OAuth2 Application CRUDが現行公開APIと不整合

- Severity: Medium
- 該当箇所: `oauth2.go`

旧`oauth2/applications` CRUDを公開する一方、現行のGet/Edit Current Applicationが不足する。

推奨:

- 公開Bot APIに掲載されていないrouteを削除/deprecated化する。
- `/applications/@me`のGET/PATCHを実装する。

## DGO-API-018: 大きなREST機能群が欠落

- Severity: Medium

不足する代表例:

- Lobby全endpoint/model
- Voice HTTP voice-state GET/PATCH
- Bulk Guild Ban
- Get Guild Role
- Get Guild Role Member Counts
- Guild Voice Regions
- Widget Public
- Vanity URL
- Welcome Screen
- Incident Actions
- Group DM recipient add/remove
- Create Group DM
- Invite target-users
- Get Sticker Pack

## DGO-API-019: Role gradient colors未対応

- Severity: Medium
- 該当箇所:
  - `structs.go:1406`
  - `structs.go:1479`

deprecated `color`のみで、現行`colors` objectがない。

## DGO-API-020: User modelの現行field不足

- Severity: Medium
- 該当箇所: `user.go:44`以降

不足:

- `avatar_decoration_data`
- `collectibles`
- `primary_guild`

## DGO-API-021: Application / Interaction modelの現行field不足

- Severity: Medium
- 該当箇所:
  - `structs.go:162-182`
  - `interactions.go:221`以降

Applicationで不足する代表例:

- bot
- guild
- flagsの新field
- approximate counts
- redirect URIs
- interactions endpoint URL
- event webhooks
- tags
- install params
- custom install URL

Interactionで不足する代表例:

- partial guild
- partial channel
- attachment size limit

## DGO-API-022: Reaction model/RESTが不完全

- Severity: Medium
- 該当箇所:
  - `message.go:460`
  - `restapi.go:2729`

不足:

- `count_details`
- `me_burst`
- `burst_colors`
- reaction `type` query

burst reaction userを正しく取得できない。

## DGO-API-023: Message type/modelが古い

- Severity: Medium
- 該当箇所:
  - `message.go:45`
  - `message.go:49`以降

MessageTypeが23付近で止まり、24、25、26、27、28、29、31、32、36-39、44、46等がない。

Messageで不足する代表field:

- `application_id`
- `role_subscription_data`
- `resolved`
- `call`
- `shared_client_theme`

## DGO-API-024: GuildTemplateCreateがerrorを返さない

- Severity: Medium
- 該当箇所: `restapi.go:1726`

戻り値にerrorがなく、REST request errorとJSON unmarshal errorを握り潰す。

推奨:

- `(*GuildTemplate, error)`へ変更する。

## DGO-API-025: Endpointに二重slashがある

- Severity: Medium
- 該当箇所:
  - `endpoints.go:48`
  - `endpoints.go:156`

`EndpointAPI`自体が末尾slashを持つのに、以下はさらに`/`を追加する。

- `/api/v10//voice/`
- `/api/v10//sticker-packs`

proxy/routerによってredirect、404、signature/cache key差異が起こり得る。

## DGO-API-026: Audit Log API/enumが不足

- Severity: Medium
- 該当箇所: `restapi.go:1407`

- GuildAuditLogはbeforeのみでafterに未対応。
- action enumが新しいvoice status、soundboard等に未対応。

## DGO-API-027: Discord Rate Limit bucket scopeの扱いが不完全

- Severity: Medium
- 該当箇所: `ratelimit.go`

`X-RateLimit-Scope`を扱わず、user/global/sharedの違いをeventやpolicyに反映しない。

invalid request limitの監視もない。

推奨:

- 401/403/429 rate metricを追加する。
- 10,000 invalid requests / 10 minutesへ近づいた場合の警告・circuit breakerを追加する。

---

# 7. discord.py 2.7.1との比較

## 7.1 比較上の注意

dgoはlow-level Go binding、discord.pyは高水準frameworkを含むため完全な1対1比較ではない。

それでも、Gateway state machine、rate limit、sharding、current API model等、low-level libraryとして比較可能な領域でもdgoに大きな不足がある。

比較対象:

- [discord.py 2.7.1 PyPI](https://pypi.org/project/discord.py/)
- [discord.py API Reference](https://discordpy.readthedocs.io/en/stable/api.html)
- [discord.py Changelog](https://discordpy.readthedocs.io/en/stable/whats_new.html)

## 7.2 dgoで不足する安全性・基盤機能

| 機能 | discord.py 2.7.1 | dgo |
| --- | --- | --- |
| OP9 bool分岐 | 対応 | 未対応 |
| resume_gateway_url | 保存・利用 | field自体なし |
| fatal close code | 停止/専用error | 全て再接続 |
| Gateway send limiter | あり | なし |
| Identify concurrency | shard schedulerあり | なし |
| AutoSharding | あり | 手動Sessionのみ |
| HTTP retry | bounded loop | 502のみ、429は無制限再帰 |
| rate bucket eviction | あり | なし |
| event callback error hook | あり | panicでprocess終了 |
| global AllowedMentions | あり | なし |
| Client close時Voice cleanup | あり | TODO |

discord.py source上の参考:

- [Gateway state machine](https://github.com/Rapptz/discord.py/blob/v2.7.1/discord/gateway.py)
- [HTTP rate limit/retry](https://github.com/Rapptz/discord.py/blob/v2.7.1/discord/http.py)

## 7.3 dgoで不足するDiscord機能

- Soundboard REST/cache/events
- Voice Channel Effects
- Modal Label/FileUpload/RadioGroup/CheckboxGroup/Checkbox
- Current permission flags
- Role Member Counts
- Current application editing
- complete entitlement handling
- modern Message/User/Application fields
- Gateway OP31/43
- Gateway RATE_LIMITED event

## 7.4 discord.pyが持つ高水準機構

dgoには以下に相当する標準機構がない。

- `discord.ext.commands`
  - commands
  - groups
  - cogs
  - extensions
  - checks
  - converters
  - cooldowns
  - help command
- `discord.ext.tasks`
- `app_commands.CommandTree`
  - decorator
  - localization
  - sync
  - checks
  - install/context control
- `ui.View` / `LayoutView`
  - persistent component dispatch
  - timeout
  - callback routing
- `wait_for`
- AutoShardedClient
- member cache policy
- shard lifecycle event

これはlow-level libraryとして必須ではないが、「discord.pyの代替」として見る場合は大きなDX差になる。

## 7.5 dgo側が多い低水準機能

- Voice Opus受信
- Manual Gateway write
- Raw REST request
- Interaction HTTP署名helper
- 低水準Voice/DAVE hook

ただしDAVEはdiscord.py 2.7.0以降で正式対応されたため、現在はdgo固有の優位点ではない。

## 7.6 discord.pyより危険な過剰API

- InviteAccept
- user tokenを拒否しないcredential設計
- UserUpdate/UserConnections等のuser/OAuth2面とGateway Sessionの混在
- 旧OAuth2 application CRUD
- 廃止済みGuild/Integration API

discord.pyはuser token loginをToS違反として扱い、user-only endpointを削除する方向である。

## 7.7 追随済み・同等に近い領域

dgoで既に実装されているもの:

- Components v2
  - Section
  - TextDisplay
  - Thumbnail
  - MediaGallery
  - File
  - Separator
  - Container
- Poll
- Poll vote events
- Entitlement/Subscriptionの基本model
- Application emojis
- Message forwarding reference
- DAVEの基本実装
- Audit log entry event
- Raw Event fallback

ただし、実装済みでもpagination型、DAVE fail-open、model field不足等のcorrectness問題が残る。

---

# 8. Repository・CI・Release・Documentation

## DGO-REPO-001: Root testは成功

実行結果:

- `go test ./...`: 成功
- `go test -race ./...`: 成功
- `go vet ./...`: 成功
- `go mod verify`: 成功

注意:

- 多くのintegration testはDiscord token/environmentがない場合skipされる。
- race detectorが成功しても、Voice/DAVE/event concurrency pathのtest coverageが不足する。

## DGO-REPO-002: Coverageが低い

測定結果:

- root package: 10.5%
- repository全体: 9.1%
- Voice/DAVE/MLS: 実質0%
- examples: 0%

重大問題の多くがVoice、DAVE、Gateway reconnect、error pathに集中しているため、現在のcoverageではCI greenが安全性を示さない。

推奨:

- fake Gateway server
- fake Voice Gateway
- UDP/RTP fuzz
- OP9/OP7/close code table test
- heartbeat timing test
- DAVE state transition fixture
- rate limit header/429 test
- handler deadlock/race test

## DGO-REPO-003: Nested example modulesがroot CIから除外され、単独buildに失敗

対象:

- `examples/linked_roles`
- `examples/voice_receive`

両directoryは独立`go.mod`を持つため、rootの`go test ./...`対象外。

単独`go test ./...`結果:

- missing `go.sum` entry for `github.com/cloudflare/circl/hpke`
- build失敗

さらにnested module pathは旧`github.com/darui3018823/discordgo/examples/...`のまま。

推奨:

- CI matrixでrootと全nested moduleを列挙する。
- `go mod tidy`結果をcommitする。
- module pathをdgoへ統一する。

## DGO-REPO-004: READMEとgo.modのGo要件が不一致

- `README.md`: Go 1.21+
- `go.mod`: Go 1.24.0

CI matrixにはGo 1.23があるが、Go toolchain auto-downloadにより実際には1.24 toolchainを取得している可能性があり、「Go 1.23でnativeにcompile可能」という検証にはならない。

推奨:

- minimum Go versionを1つに決める。
- README、go.mod、CI、release workflowを統一する。
- 古いGoでのtoolchain auto-downloadを禁止したtestも検討する。

## DGO-REPO-005: VERSIONがreleaseと不一致

- `discord.go`: `VERSION = "0.30.0"`
- 最新release: `v0.30.6`
- HEAD: v0.30.6以後のuntagged commit

User-Agentは全0.30.x releaseで0.30.0を報告する。

影響:

- incident調査時のversion識別不能
- telemetry/logの誤情報
- bug reportの混乱

推奨:

- build info/debug.ReadBuildInfoを利用する。
- release時にversionを自動生成する。
- hard-coded VERSIONをsource of truthにしない。

## DGO-REPO-006: Documentationが旧discordgoのまま

例:

- `docs/index.md`: DiscordGo表記
- 旧repository URL
- 旧import path
- `docs/GettingStarted.md`: Go 1.4以上
- examples READMEのDiscordGo表記
- package/file headerのDiscordgo表記

推奨:

- docs全体をdgoへ移行する。
- READMEを現行APIとGo versionへ合わせる。
- unsupported/deprecated API一覧を公開する。
- security warning、self-bot禁止、recording privacyを追加する。

## DGO-REPO-007: Branch filterがdefault branchと不一致

default branchは`master`。

一方:

- `.github/workflows/codeql.yml`のpush/PR: `main`
- `.github/workflows/go.yml`のpush/PR: `main`

CodeQL scheduleはdefault branchのmasterで毎日成功しているが、masterへのpush/PRではこれら2 workflowが起動しない。

通常の`CI`と`golangci-lint` workflowはmasterで動作するため、全CIが停止しているわけではない。

推奨:

- branch名をmasterに統一する。
- またはdefault branchをmainへ移行し全設定を更新する。

## DGO-REPO-008: ReleaseがCI/lint成功にgateされていない

tag pushでCreate Release workflowが独立して動き、独自の`go test`だけでreleaseを作成する。

実例:

- v0.30.4 Create Release: 成功
- 同commitのCI: 失敗
- 同commitのgolangci-lint: 失敗

参照:

- [v0.30.4 release run](https://github.com/darui3018823/dgo/actions/runs/26580402334)
- [v0.30.4 failed CI](https://github.com/darui3018823/dgo/actions/runs/26580402458)

推奨:

- releaseをreusable verification workflow成功後にのみ実行する。
- race、vet、lint、nested module、govulncheckをrelease gateに含める。
- tagとsource versionの一致を検証する。

## DGO-REPO-009: Release workflowのGo versionも不一致

Create ReleaseはGo 1.21をsetupする一方、moduleはGo 1.24を要求する。

Go toolchain auto-downloadで成功し得るが、workflow記述と実際のcompilerが一致しない。

推奨:

- root go.modからversionを取得する。
- `actions/setup-go`の`go-version-file: go.mod`を使用する。

## DGO-REPO-010: Dependabot security alertsがdisabled

`.github/dependabot.yml`は存在し、dependency update PR設定もあるが、GitHub repository側のDependabot alertsはdisabledだった。

推奨:

- Dependabot alerts/security updatesを有効にする。
- update PRとsecurity advisory detectionを分けて確認する。

## DGO-REPO-011: 到達可能な既知脆弱性は0だがdependency更新余地あり

`govulncheck`結果:

- 到達可能なvulnerability: 0
- imported package vulnerability: 0
- required module内の非到達vulnerability: 15

主に以下:

- `golang.org/x/crypto v0.46.0`
- `golang.org/x/sys v0.39.0`

audit時点の更新候補:

- `github.com/cloudflare/circl v1.6.3 -> v1.6.4`
- `golang.org/x/crypto v0.46.0 -> v0.54.0`
- `golang.org/x/sys v0.39.0 -> v0.47.0`
- transitive `x/net`、`x/term`、`x/text`

到達不能であるため直ちにexploitableとは判定しないが、dependency hygieneとして更新を推奨する。

## DGO-REPO-012: SECURITY.mdがない

脆弱性を非公開で報告する正式経路、supported versions、response policyがない。

推奨:

- `SECURITY.md`を追加する。
- GitHub private vulnerability reportingを有効にする。
- supported release lineとresponse SLAを記載する。

## DGO-REPO-013: CodeQL alertのdismiss運用に注意が必要

確認された履歴:

- request forgery alert: dismissed
- clear-text logging alert: dismissed
- OAuth state alert: fixed
- cookie Secure flag alert: dismissed

request forgeryはvoice endpoint allowlistが追加されているため一定のmitigationがある。

clear-text header loggingはAuthorization/Cookie redactionが追加されたが、本監査で別経路のtoken loggingが多数見つかった。

cookie alertは`examples/linked_roles/main.go`で`Secure`が現在も設定されていないため、dismiss commentと現コードの状態が一致しない。

推奨:

- dismissed alertを定期的に再監査する。
- 「別経路で同種の問題が残っていないか」を確認する。
- fix commitをsecurity regression testで固定する。

## DGO-REPO-014: linked_roles exampleにWeb security問題

- Severity: Medium
- 該当箇所:
  - `examples/linked_roles/main.go:74-146`
  - `examples/linked_roles/main.go:149-165`

問題:

- callbackで`q["code"][0]`をlength checkせず参照し、条件次第でpanic。
- OAuth state cookieに`Secure`がない。
- `SameSite`がない。
- state cookieをcallback成功後にclearしない。
- `http.ListenAndServe`にReadHeaderTimeout等がない。
- server errorを無視する。
- OAuth exchange errorをそのままHTTP responseへ出す。
- 301 redirectを使用し、unique state付きURLのcache挙動が複雑。

推奨:

- code/state queryを厳密に検証する。
- Secure、HttpOnly、SameSite、短いMaxAgeを設定する。
- callback後にcookieをexpireする。
- `http.Server`へ各timeoutを設定する。
- client向けerrorとinternal logを分離する。

## DGO-REPO-015: GitHub Actionsの重複とversion混在

workflow:

- CI
- Go
- golangci-lint
- CodeQL
- Create Release

CIとGo workflowでbuild/test責務が重複する一方、branch filterが異なる。

actions/setup-goとactions/checkoutもv3からv6まで混在する。

推奨:

- verification workflowを1つのreusable workflowへ集約する。
- action major versionを揃える。
- actionをcommit SHA pinするか、Dependabotで更新を管理する。

## DGO-REPO-016: staticcheck警告

staticcheckでは計8件の品質警告を確認した。

分類:

- ST1005
- S1023
- S1002
- ST1011

致命的ではないが、error style、冗長code、comment conventionの品質問題が残る。

## DGO-REPO-017: LicenseがGitHub上でOther判定

LICENSEは実質BSD-3-Clause系だが、追加のcopyright注記等によりGitHubのlicense detectorは`Other`として認識した。

影響:

- 利用者がlicenseを自動判定しにくい。
- package/OSS scannerでunknown licenseになる可能性。

推奨:

- SPDX-compatibleなBSD-3-Clause本文を維持する。
- fork固有注記は別NOTICEへ移すことを検討する。
- 法的判断が必要な場合は専門家へ確認する。

## DGO-REPO-018: upstreamとの差分

audit時点のlocal refs比較:

- fork側のみ: 50 commits
- upstream側のみ: 5 commits

未取り込みの代表:

- File Upload Component
- Guild Role Member Counts
- Message Reaction Remove Emoji event
- AES-256-GCM Voice transport
- SelectMenu/TextInput Required field修正

推奨:

- upstream cherry-pick可否を個別評価する。
- DAVE/voice変更とのconflictをinterop testする。
- upstream sync policyを文書化する。

## DGO-REPO-019: package naming・API surfaceの移行が不完全

source header、comment、docs、nested module、example名にDiscordGo表記が残る。

またdeprecated compatibility field/APIが多数残り、hard forkとしての新しいpublic contractが明確でない。

推奨:

- compatibility policyとmajor release planを作る。
- deprecated itemへ期限を設定する。
- generated API inventoryをreleaseごとに公開する。

---

# 9. その他の品質・設計上の問題

## DGO-QUALITY-001: Session loggerのLogLevel契約が不明確

Session loggerは`TryRLock`失敗時にlogを捨てる。

Open/Close等がSession write lockを保持している間の重要logが出力されない可能性がある。

また旧LogLevelとslog levelの責務が混在し、SessionとVoiceでlogging implementationが異なる。

推奨:

- logger pointer/configを別mutexまたはatomic valueで管理する。
- Session state lockとloggingを分離する。
- LogLevel contractをslogへ統一する。

## DGO-QUALITY-002: RESTErrorがRequest/Response全体を保持する

`RESTError`はAuthorization headerを含む`*http.Request`を保持する。

`Error()`自体はbodyのみを返すが、利用者が`%+v`等でstructを出力した場合にcredentialが露出する可能性がある。

推奨:

- sanitized request metadataだけを保持する。
- tokenを含むheaderやURLをerror objectへ残さない。

## DGO-QUALITY-003: REST response bodyにsize limitがない

`io.ReadAll(resp.Body)`でDiscord/proxy responseを無制限に読む。

Discord公式serverを前提にすれば通常は制御されるが、custom Client、proxy、exported endpoint変更、compromised network pathでmemory圧迫を受け得る。

推奨:

- endpoint classごとに十分大きい上限を設定する。
- file/CDN downloadはstreaming APIを分ける。

## DGO-QUALITY-004: Open/reconnectのcancel modelがない

初回OpenWithContext以外のreconnectは`context.Background()`を使い、利用者がongoing reconnectをcancelする一貫した方法がない。

推奨:

- Session lifetime contextを持つ。
- Open、listener、heartbeat、reconnect、Voiceへ同じcancel treeを伝播する。

## DGO-QUALITY-005: Closeのidempotency/event semanticsが曖昧

接続がない状態でもDisconnect eventを発火し得る。並行Close/reconnect時のevent重複や順序が明文化されていない。

推奨:

- connection state enumを導入する。
- state transitionごとにConnect/Disconnectを一度だけ発火する。

## DGO-QUALITY-006: unknown Gateway eventの扱い

Raw Event fallbackがあるためforward compatibilityの逃げ道は存在する。

一方、unknown eventをwarningとして大量logするため、新event追加時に不要なwarning stormが起こり得る。

推奨:

- unknown eventをdebug/infoへ下げるoptionを用意する。
- metricとraw event callbackを維持する。

---

# 10. 検証結果

## 10.1 Local commands

| Command | Result |
| --- | --- |
| `go version` | `go1.26.5 windows/amd64` |
| `go test ./...` | 成功 |
| `go test -race ./...` | 成功 |
| `go vet ./...` | 成功 |
| `go mod verify` | 成功 |
| `govulncheck ./...` | 到達可能な脆弱性0 |
| coverage | root 10.5%、全体9.1% |
| staticcheck | 8 warnings |
| linked_roles module test | 失敗 |
| voice_receive module test | 失敗 |

## 10.2 GitHub Actions

- latest master CI: 成功
- latest master golangci-lint: 成功
- daily CodeQL scheduled run: 成功
- Go workflow: master pushではbranch filter不一致により起動しない
- CodeQL push/PR: masterではbranch filter不一致
- release workflow: verification workflowと独立

## 10.3 Worktree

監査中はsource codeを変更していない。

本レポートのみを追加した。

---

# 11. 推奨修正ロードマップ

## Phase 0: Release停止・安全確保

1. 新releaseを一時停止する。
2. Debug logのtoken/secret出力を即時停止する。
3. `InviteAccept`をdeprecated化し、常時errorにする。
4. SECURITY.mdとprivate vulnerability reportingを用意する。
5. Voice recording exampleへ明確な警告を追加する。

## Phase 1: Gateway state machine

1. OP9 bool分岐を実装する。
2. fatal/resumable/fresh-identify close code tableを実装する。
3. `resume_gateway_url`を保存する。
4. HELLO直後にheartbeat/listenerを開始する。
5. 初回heartbeat jitterと1周期ACK判定を実装する。
6. Gateway write queue/rate limiterを追加する。
7. Identify concurrency/shard schedulerを追加する。
8. Session lifetime contextとcancelを導入する。

## Phase 2: Voice/DAVE

1. Voice event処理を直列化する。
2. `wsListen`で引数connectionを読む。
3. Voice OP6 ACK、OP11 Clients Connect、close code tableを実装する。
4. RTP parserを全面的にbounds-checkする。
5. DAVE active中をfail-closedにする。
6. DAVE proposal/commit/state transitionを完成させる。
7. Session.Closeから全Voice resourceを終了する。
8. Voice/DAVE/UDP fuzz・interop testを追加する。

## Phase 3: REST・Concurrency

1. REST 429 recursionをbounded loopへ変更する。
2. global/route rate waitを再設計する。
3. reaction hard-coded limiterを削除する。
4. bucket hashへmajor parameterを含める。
5. bucket evictionを追加する。
6. 全2xx成功、transient 5xx bounded retryを実装する。
7. RequestRawとpublic low-level APIへ入力検証を追加する。
8. Interaction request bodyを制限する。
9. event handlerをlock外で呼び出す。
10. handler panic hookを追加する。

## Phase 4: Current Discord API correctness

優先順:

1. Entitlement/Subscription/joined thread pagination型
2. Application JSON tag
3. Stage event wire名
4. Request Guild Members payload/rate limit
5. New Modal components
6. Paginated Pinsとpermission
7. Soundboard
8. Voice Channel Info
9. Entry Point/Launch Activity
10. Message/User/Application/Role/Reaction model更新

## Phase 5: Deprecated API cleanup

削除/deprecated対象:

- InviteAccept
- GuildCreate
- GuildCreateWithTemplate
- ThreadsActive(channel)
- GuildIntegrationCreate/Edit
- old OAuth2 application CRUD
- disabled command permissions batch edit
- compatibility field群

major releaseでの破壊的変更を推奨する。

## Phase 6: Repo/Release

1. nested modulesをCIへ追加する。
2. minimum Go versionを統一する。
3. VERSIONをbuild metadata化する。
4. master/main filterを統一する。
5. releaseをCI/race/vet/lint/vuln成功後にgateする。
6. Dependabot alertsを有効にする。
7. dependencyを更新する。
8. docs/import path/package nameをdgoへ統一する。
9. coverage thresholdを段階的に導入する。

---

# 12. 修正完了条件

次の条件を満たすまでは「監査対応完了」としないことを推奨する。

- OP9 true/false fixture testが成功する。
- fatal close codeで再接続しない。
- resume URLを使う。
- heartbeatがHELLO後かつREADY前に開始される。
- Gateway send limiterが120/60秒を超えない。
- Voice HELLO/READY逆転でpanicしない。
- malformed RTP fuzzでpanicしない。
- DAVE active中のencrypt/decrypt failureで平文fallbackしない。
- debug logにtoken、webhook path token、voice secret keyが出ない。
- Interaction body上限testが成功する。
- handler内Add/Removeでdeadlockしない。
- nested modulesを含むCIが成功する。
- deprecated/private routeを通常利用できない。
- current modal、pins、permission、pagination fixtureが成功する。
- race detectorとgovulncheckがrelease workflowで成功する。

---

# 13. 主要公式資料

## Discord

- [Gateway](https://docs.discord.com/developers/events/gateway)
- [Gateway Events](https://docs.discord.com/developers/events/gateway-events)
- [Opcodes and Status Codes](https://docs.discord.com/developers/topics/opcodes-and-status-codes)
- [Voice Connections](https://docs.discord.com/developers/topics/voice-connections)
- [Rate Limits](https://docs.discord.com/developers/topics/rate-limits)
- [Message Resource](https://docs.discord.com/developers/resources/message)
- [Channel Resource](https://docs.discord.com/developers/resources/channel)
- [Guild Resource](https://docs.discord.com/developers/resources/guild)
- [Invite Resource](https://docs.discord.com/developers/resources/invite)
- [Application Resource](https://docs.discord.com/developers/resources/application)
- [Entitlement Resource](https://docs.discord.com/developers/resources/entitlement)
- [Subscription Resource](https://docs.discord.com/developers/resources/subscription)
- [Soundboard Resource](https://docs.discord.com/developers/resources/soundboard)
- [Component Reference](https://docs.discord.com/developers/components/reference)
- [Permissions](https://docs.discord.com/developers/topics/permissions)
- [Discord Developer Change Log](https://docs.discord.com/developers/change-log)
- [Discord Developer Policy](https://support-dev.discord.com/hc/en-us/articles/8563934450327-Discord-Developer-Policy)
- [Discord Developer Terms of Service](https://support-dev.discord.com/hc/en-us/articles/8562894815383-Discord-Developer-Terms-of-Service)
- [Automated User Accounts (Self-Bots)](https://support.discord.com/hc/en-us/articles/115002192352-Automated-User-Accounts-Self-Bots)

## discord.py

- [discord.py 2.7.1](https://pypi.org/project/discord.py/)
- [API Reference](https://discordpy.readthedocs.io/en/stable/api.html)
- [Interactions API Reference](https://discordpy.readthedocs.io/en/stable/interactions/api.html)
- [Changelog](https://discordpy.readthedocs.io/en/stable/whats_new.html)
- [Gateway implementation](https://github.com/Rapptz/discord.py/blob/v2.7.1/discord/gateway.py)
- [HTTP implementation](https://github.com/Rapptz/discord.py/blob/v2.7.1/discord/http.py)

## Repository

- [dgo GitHub Actions](https://github.com/darui3018823/dgo/actions)
- [dgo latest release](https://github.com/darui3018823/dgo/releases/tag/v0.30.6)
- [upstream discordgo](https://github.com/bwmarrin/discordgo)

