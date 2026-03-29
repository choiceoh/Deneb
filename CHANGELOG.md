# Changelog

## [3.30.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.29.0...deneb-v3.30.0) (2026-03-29)


### ✨ Features

* **aurora:** add memory transfer from Aurora summaries to long-term memory ([e2f540d](https://github.com/choiceoh/Deneb/commit/e2f540d5db45e554c732f554c5a8e4923d0ea5da))
* **chat:** integrate per-channel upload-file limits ([3bde736](https://github.com/choiceoh/Deneb/commit/3bde736022bc450aed82107af7ccb312b81f0072))
* **llm:** add shared httpretry policy with per-code retry classification ([b734193](https://github.com/choiceoh/Deneb/commit/b7341932725e7e5ca8eb6bc9225930905b6e42de))
* **memory:** flush important facts to memory before Aurora compaction ([c4d6ad5](https://github.com/choiceoh/Deneb/commit/c4d6ad599b823c1e7ea6e3aa4e3b98e23d80a58c))
* **session:** restore gateway sessions and send startup heartbeat on restart ([8112e3c](https://github.com/choiceoh/Deneb/commit/8112e3c475c23dd57e03ef6d303a70f87ac706ee))
* **status:** expose specific error reasons in daemon status and error messages ([1b85063](https://github.com/choiceoh/Deneb/commit/1b8506323d44afad34d66b689c27e67dd81453aa))
* **telegram:** validate photo metadata and fall back to document on failure ([29c7f99](https://github.com/choiceoh/Deneb/commit/29c7f99df36b43b66ac0a1002d91217d89410940))


### 🐛 Bug Fixes

* **chat:** omit empty runId from logs, abbreviate session channel prefix ([4c21416](https://github.com/choiceoh/Deneb/commit/4c214160e5da6ff992f3aa2d45e421489ad16a5b))
* **logging:** drop fractional seconds from console timestamp ([cc9c929](https://github.com/choiceoh/Deneb/commit/cc9c9297b8696dc9965114567c6a95dcc9f33780))
* **logging:** replace level text with bar color, drop level label ([c9fc432](https://github.com/choiceoh/Deneb/commit/c9fc432c9197dcd1fd4d0ed06c60d9f67a2e8b45))


### 🔧 Internal

* **core:** replace 14-pass HTML-to-Markdown with 2-pass tokenizer + emitter ([611946d](https://github.com/choiceoh/Deneb/commit/611946d87ed3a08e86a5a6d10adad92b875f2abe))

## [3.29.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.28.1...deneb-v3.29.0) (2026-03-29)


### ✨ Features

* **core:** improve html-to-markdown conversion with rich formatting support ([9f43451](https://github.com/choiceoh/Deneb/commit/9f4345166b9e896a25a4cd174c07cbeb5b2abdc1))


### 🐛 Bug Fixes

* **chat:** increase abort GC expiry from 30m to 4h to prevent premature session kill ([a32dfad](https://github.com/choiceoh/Deneb/commit/a32dfad0d4c20d91394c0d5583cf1d9e508144d7))
* **discord:** harden chunk splitting edge cases ([2540fc1](https://github.com/choiceoh/Deneb/commit/2540fc1b0ea8eb0f0dd36fec7013e61398841a11))
* **discord:** resolve undefined autoreply.FinalizeInboundContext ([3135830](https://github.com/choiceoh/Deneb/commit/3135830564c76ef48517a7750373fab9d1397da5))


### 🔧 Internal

* **agent-runtime:** clean up session_keys.rs structure ([168b0e8](https://github.com/choiceoh/Deneb/commit/168b0e816a39c520c34c0fba7b16cece53736fce))
* **agent:** unify dual LLM agent loop engines into internal/agent ([8e42b06](https://github.com/choiceoh/Deneb/commit/8e42b061e467ae56db694fbe6c87f2f0089eb7a0))
* **autoreply:** bootstrap rules/cmd-dispatch/subagent facades ([e106769](https://github.com/choiceoh/Deneb/commit/e10676912ff0936bf5efe8afdfb2fce66b2aba3b))
* **autoreply:** convert split action files to facades in handlers layer ([5a8b365](https://github.com/choiceoh/Deneb/commit/5a8b3654791fdc1c56254f4ead112fe8963b4a10))
* **autoreply:** migrate subagent command layer out of handlers into subagent package ([8e04be7](https://github.com/choiceoh/Deneb/commit/8e04be74c6cbc92b7209bd4a81f206bd9857484d))
* **autoreply:** migrate subagent shared logic into subagent package ([322e9ea](https://github.com/choiceoh/Deneb/commit/322e9eabad7e43157566a4000a65f82a8280541d))
* **autoreply:** remove remaining compat shims ([8c1ee46](https://github.com/choiceoh/Deneb/commit/8c1ee46c7f117a7ba59c177c9e472c8b84552144))
* **autoreply:** remove remaining compat shims and migrate callers to subpackages ([cd6e792](https://github.com/choiceoh/Deneb/commit/cd6e792f92692863f811692f9337c73323d68c93))
* **autoreply:** rename commands/ subpackage to handlers/ ([2bd713d](https://github.com/choiceoh/Deneb/commit/2bd713d8101af0746ecde354c425264a3aea877d))
* **autoreply:** split agent runner into focused files ([7bcab8a](https://github.com/choiceoh/Deneb/commit/7bcab8a2827a97efc08b5503574f2917aa44d085))
* **autoreply:** split agent runner into focused files ([e0832be](https://github.com/choiceoh/Deneb/commit/e0832be2d5a99a57bedc162a71c2ecdca6152de7))
* **autoreply:** split dispatch and pipeline subpackages with compat shims ([f8eba5c](https://github.com/choiceoh/Deneb/commit/f8eba5ca32c76c9fe2a0132490e5c26c3eb716e8))
* **autoreply:** start dispatch/pipeline subpackage split ([1cf0411](https://github.com/choiceoh/Deneb/commit/1cf0411761ab5ca32c55932fbe17f20546185e28))
* **bootstrap:** extract gateway and CLI startup into bootstrap packages ([9904e12](https://github.com/choiceoh/Deneb/commit/9904e127ef47c5c1d7fce6cbf0bd8dfa9994533f))
* **channel:** extract PluginBase and RunState to eliminate lifecycle boilerplate ([8979f20](https://github.com/choiceoh/Deneb/commit/8979f203bbd9a070653f16f60707b028d37709fa))
* **chat:** decompose CoreToolDeps into focused per-domain dep structs ([8111dec](https://github.com/choiceoh/Deneb/commit/8111dec20eaea2bfa243ef97ab4df73bf6ad5486))
* **chat:** remove deleted polaris_* and chat.go source files ([2f1c6ab](https://github.com/choiceoh/Deneb/commit/2f1c6ab1d0cfd1598ea48a24e7436d749bd8976f))
* **chat:** rename tools_*.go → toolreg_*.go for naming clarity ([095e557](https://github.com/choiceoh/Deneb/commit/095e5577fd57344c486f3c1309c4f5cc2f55aa9f))
* **chat:** split chat.go and move polaris_* to chat/polaris subpackage ([88fd100](https://github.com/choiceoh/Deneb/commit/88fd1000027a341116e0601429a871a00562967b))
* **chat:** split core tool implementations into tools subpackage ([5a1dee9](https://github.com/choiceoh/Deneb/commit/5a1dee9bf66f67038bed6565af52f55ff8001d56))
* **chat:** split foundational tool implementations into internal/chat/tools ([62f5223](https://github.com/choiceoh/Deneb/commit/62f522398097c0eff02848c8f3c8934dbc769e74))
* **cli:** add shared gateway query pipeline ([acb886f](https://github.com/choiceoh/Deneb/commit/acb886f91547c01d09f045183833970ffa991fd0))
* **cli:** clarify env/config path/io layering ([e469ee6](https://github.com/choiceoh/Deneb/commit/e469ee6f5c4e1613905a8db42c5e408dc64c97c9))
* **cli:** delegate command dispatch to domain router ([543538b](https://github.com/choiceoh/Deneb/commit/543538be6c53d514bc3ca5248bd11c3e6bfc2634))
* **cli:** route commands through domain dispatch ([2aa15c5](https://github.com/choiceoh/Deneb/commit/2aa15c53ce8550f75d3f45d9a9a0bedeb94c3a5d))
* **cli:** split agents command into focused modules ([4407d12](https://github.com/choiceoh/Deneb/commit/4407d1226557093a76c28d5d0fd0050c208407d3))
* **cli:** split agents command into purpose-specific modules ([1156f4e](https://github.com/choiceoh/Deneb/commit/1156f4ec658f0a798bf74674b0e50a54b053e5e9))
* **cli:** split bootstrap and router from main entrypoint ([1d061d3](https://github.com/choiceoh/Deneb/commit/1d061d398b5acf9dbce180dcbbbefa83a0a31f2e))
* **cli:** split bootstrap and routing from main entrypoint ([905d07c](https://github.com/choiceoh/Deneb/commit/905d07c50e076f22fad18964c3ab41ddd2c1b1a3))
* **cli:** split env/path/io config responsibilities ([65f9566](https://github.com/choiceoh/Deneb/commit/65f9566c64c291841ea61f6facc8a9ef8638532e))
* **cli:** 공통 gateway query 실행 파이프라인 도입 ([9a7b8c3](https://github.com/choiceoh/Deneb/commit/9a7b8c38a34d647239c4e2f9b01b0018aa48c3f6))
* **config:** add legacy_compat and paths modules (new files) ([8884627](https://github.com/choiceoh/Deneb/commit/8884627013479b51bf3fd14694d0ee0d7e4787bf))
* **config:** modularize path resolution into policy objects ([e56d0cb](https://github.com/choiceoh/Deneb/commit/e56d0cb955547356e9e389f85bc6c241018dcf60))
* **core:** add FFI string macros and apply in lib exports ([869e653](https://github.com/choiceoh/Deneb/commit/869e6539cd8483d3d7780b017cc720d5a1914e9f))
* **core:** extract reusable string-based FFI macros in lib exports ([9d9a0f5](https://github.com/choiceoh/Deneb/commit/9d9a0f51f5c85a1b13d3b5bbcfd1b042a2e45fa7))
* **cron:** split gateway cron service by responsibility ([c6e6d4e](https://github.com/choiceoh/Deneb/commit/c6e6d4e6d2f28e776a7b0285759a033c2cb2d1cd))
* **cron:** split service responsibilities into focused files ([e0aa090](https://github.com/choiceoh/Deneb/commit/e0aa09018fe6d94ee96d850424d8493f7ab99543))
* **ffi:** separate FFI layer into domain submodules under ffi/ ([eaf0438](https://github.com/choiceoh/Deneb/commit/eaf0438e816e545a89394184f90e8370ad3b8b8d))
* **gateway-go:** split ffi RPC handlers by domain ([53627ea](https://github.com/choiceoh/Deneb/commit/53627ea00ab564703147ad3b1d65e8cdc6eb781a))
* **gateway-go:** split ffi RPC handlers by domain ([a22ad50](https://github.com/choiceoh/Deneb/commit/a22ad503800b996751b91d43bbab90cf9eb42ca9))
* **gateway-rpc:** extract gateway runtime handlers and split Phase2 registration wiring ([82f920c](https://github.com/choiceoh/Deneb/commit/82f920c30bcda08d0ff91effb7170998abd4cbc7))
* **gateway-rpc:** extract runtime handlers and phase2 wiring helpers ([8870516](https://github.com/choiceoh/Deneb/commit/8870516c1a94c5b219c035f35e4b77ae47195bf5))
* **go:** separate web fetch into http/html/content layers ([510d0ce](https://github.com/choiceoh/Deneb/commit/510d0ce5fc373ce023500c262f46b5e6f0f7f41d))
* **go:** split web fetch into http/html/content layers ([34af112](https://github.com/choiceoh/Deneb/commit/34af112575b9a4a82977cc7b1b8ba2835d5fc4e9))
* **markdown:** split parser.rs into render_state, spoilers modules ([2223863](https://github.com/choiceoh/Deneb/commit/2223863acc6f3845eb3bdf9a7c6efa046855459e))
* **memory:** extract dreaming phases into dreamPhase interface ([af7817d](https://github.com/choiceoh/Deneb/commit/af7817d1439d8ccb6f3460d6253a24b3d3dd5de4))
* **memory:** split store.go into focused sub-files ([fade47d](https://github.com/choiceoh/Deneb/commit/fade47d2c381b3b5bbba395c6fd946c4fe7bd6ff))
* **memory:** split store.go into focused sub-files ([197fd43](https://github.com/choiceoh/Deneb/commit/197fd431a2dc62561ff41d1da90398e39b1acc73))
* **plugin:** split discovery logic by concern ([107915d](https://github.com/choiceoh/Deneb/commit/107915df84c05da15f17cf55ce480b76a20bc377))
* **plugin:** split discovery logic into cache/scan/security files ([20edf08](https://github.com/choiceoh/Deneb/commit/20edf08ecbfc6188428780185c15d6a8518708fa))
* **rpc:** add DecodeParams/RespondOK helpers and eliminate handler boilerplate ([8bd1625](https://github.com/choiceoh/Deneb/commit/8bd1625da5ac617d107623c6b6383c83fb31df7a))
* **rpc:** extract Bind[P] middleware and migrate node/skill handlers ([c77cb9b](https://github.com/choiceoh/Deneb/commit/c77cb9b266c61fdaf2285638dfca2bc268fa962e))
* **rpc:** modularize core method registration with validation ([6cc0ffb](https://github.com/choiceoh/Deneb/commit/6cc0ffb4ac9374e7bb3683edb50ee8d31c10724f))
* **rpc:** modularize core method registration with validation ([6469e1d](https://github.com/choiceoh/Deneb/commit/6469e1d2c59e94425e5f7f7a5e23b2e99b88a42c))
* **server:** decompose gateway Server into transport/rpc/runtime/integrations ([adada7e](https://github.com/choiceoh/Deneb/commit/adada7e3ae209fa7d9b3b578bf7b7b958572c84d))
* **server:** extract PluginHTTPRouter to internal/server/pluginrouter ([bbdac87](https://github.com/choiceoh/Deneb/commit/bbdac877250ad94640ffc16e240d4db8d6d4bdab))
* **server:** extract PluginHTTPRouter to internal/server/pluginrouter ([2223863](https://github.com/choiceoh/Deneb/commit/2223863acc6f3845eb3bdf9a7c6efa046855459e))
* **server:** extract SessionManager, ChatManager, HookManager from Server struct ([7cfd7b6](https://github.com/choiceoh/Deneb/commit/7cfd7b627f4d4ca50be2ce7f3c98789888de5655))
* **server:** split gateway server state into focused components ([1b53b41](https://github.com/choiceoh/Deneb/commit/1b53b41336138e641dced92d0a0bf0a3955e72e7))
* **server:** split HTTP routing and WS session loop concerns ([4eb7247](https://github.com/choiceoh/Deneb/commit/4eb72479a106af517add389143d794f840c08b6f))
* **server:** split HTTP routing and WS session loop concerns ([d1c1583](https://github.com/choiceoh/Deneb/commit/d1c158376c973b838f858a523c1e9191d98e90a8))
* **server:** split server_rpc.go into auth, session, and channel domain files ([fb2cddf](https://github.com/choiceoh/Deneb/commit/fb2cddfd48279254e78ab60041ae2076c911baa1))
* **server:** split server_rpc.go into auth, session, and channel domain files ([5bc272f](https://github.com/choiceoh/Deneb/commit/5bc272f08c936ea1a230c673ec2b0b7ef6742e62))
* **session:** group Session fields into embedded config structs ([0f71a89](https://github.com/choiceoh/Deneb/commit/0f71a892652c8c5ebaf376eca926e35e90e637af))
* **session:** group Session fields into embedded config structs ([0552b9b](https://github.com/choiceoh/Deneb/commit/0552b9b5c538e9b062aeca3d8f44adecceed87e0))
* **tests:** replace panic branches in sweep.rs tests with Result errors ([57a11ab](https://github.com/choiceoh/Deneb/commit/57a11abe65e852421e1b407e617c40cc9331a254))
* **vega:** introduce command context for shared db access ([4e8a9b0](https://github.com/choiceoh/Deneb/commit/4e8a9b00e6e46e75fc2a2802b247cd7437b2399c))
* **vega:** introduce CommandContext and migrate show/timeline/compare/stats ([f84266d](https://github.com/choiceoh/Deneb/commit/f84266d600da50bdbd2548ad6d2f648266daee18))
* **vega:** replace ai.rs match dispatch with CommandHandler trait methods ([699eeda](https://github.com/choiceoh/Deneb/commit/699eeda85dc88fa15771d2c7ccb207018bfef0d6))
* **web:** split web fetch layer into three focused files ([265a279](https://github.com/choiceoh/Deneb/commit/265a279750f70408fb1c3acac9819500fd7a2b1c))

## [3.28.1](https://github.com/choiceoh/Deneb/compare/deneb-v3.28.0...deneb-v3.28.1) (2026-03-28)


### 🐛 Bug Fixes

* **core:** resolve clippy::expect_used warnings across workspace ([9954485](https://github.com/choiceoh/Deneb/commit/995448584541eb2a96de0a2cbdf149f36b3457e7))
* **core:** resolve clippy::expect_used warnings across workspace ([28fa335](https://github.com/choiceoh/Deneb/commit/28fa335e47953e3ca49b65a40089e21bcbf931e4))
* **plugin:** replace runtime panic with error logging in TypedHookRunner ([34a478c](https://github.com/choiceoh/Deneb/commit/34a478c3cce1bb12ad7ddaba9814d401bf740a38))


### 🔧 Internal

* **agent-runtime:** split selection.rs into focused submodules ([355879f](https://github.com/choiceoh/Deneb/commit/355879fe3b5d755a6f54a34d139a8b6ead61a126))
* **autoreply:** complete session/, directives/ subpackages with compat shims ([c80e99e](https://github.com/choiceoh/Deneb/commit/c80e99ebc940f21868d452a33a9f2337cbbdeac1))
* **autoreply:** extract commands/ subpackage with compat shim ([5ad47b2](https://github.com/choiceoh/Deneb/commit/5ad47b249f0872934571612ac0fb6fff5651b227))
* **autoreply:** extract domain subpackages — Phase 0–4 + compat layer ([f44482a](https://github.com/choiceoh/Deneb/commit/f44482a4ba202197aa44b71bd5d310ef86133691))
* **autoreply:** extract session/, directives/ subpackages; update acp callers ([9b2b746](https://github.com/choiceoh/Deneb/commit/9b2b746a017cad68bcef419af6197a40b5722fc2))
* **autoreply:** replace hardcoded xhigh/adaptive thinking model vars with YAML + codegen ([3ece1aa](https://github.com/choiceoh/Deneb/commit/3ece1aa2cf554809bfc71bdb0a2df8d1a27568c7))
* **autoreply:** update rpc acp test imports for acp/ subpackage ([0bebf48](https://github.com/choiceoh/Deneb/commit/0bebf482f9ef3e9db7e503727a527dfe06ca95e2))
* **chat:** begin sub-package extraction (web, streaming, prompt) ([5547de1](https://github.com/choiceoh/Deneb/commit/5547de186bb8dd9fb084f3226303d9cc27a0ee4d))
* **chat:** complete Track A + Track C sub-package extraction ([948569c](https://github.com/choiceoh/Deneb/commit/948569c9374650ef3a6964e48bbb0e43622c512a))
* **chat:** complete Track B — extract chat/streaming/ sub-package ([fef542a](https://github.com/choiceoh/Deneb/commit/fef542a7c5428bffda8c647409614e3f2c8cf264))
* **chat:** fix comment in prompt/system_prompt.go ([378ffe7](https://github.com/choiceoh/Deneb/commit/378ffe7a47422575a7e8c4d164424bc4090ceb30))
* **chat:** WIP sub-package extraction progress ([51c81a7](https://github.com/choiceoh/Deneb/commit/51c81a7d2e64d721d4eb6d26c356d96cf7e303f8))

## [3.28.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.27.2...deneb-v3.28.0) (2026-03-28)


### ✨ Features

* **discord:** add rate limit retry, config validation, and comprehensive format tests ([8ae2560](https://github.com/choiceoh/Deneb/commit/8ae25601da167309a5e7295133920d7d40a8cb62))
* **discord:** code block-aware splitting, file attachment processing, smart reply formatting ([231bb64](https://github.com/choiceoh/Deneb/commit/231bb647cc75e9ee524bd8f4f77cc966a10f7465))
* **discord:** per-channel workspace mapping, auto-context injection, coding quick commands ([14eab2f](https://github.com/choiceoh/Deneb/commit/14eab2face828fa9f15ad16b886e1994ff3d9084))
* **discord:** typing throttle, additive reaction support, bot presence, session TTL cleanup ([610980a](https://github.com/choiceoh/Deneb/commit/610980a6a691b1194922e06708a58c0bf63d0d1f))


### 🐛 Bug Fixes

* **chat:** cancel in-flight tool calls and memory goroutine on shutdown ([3f6e599](https://github.com/choiceoh/Deneb/commit/3f6e599258895c5c446c087b1387bb1d57a5dc68))
* **discord:** fix missing brace compile error, data race on seq with atomic, errors.As, remove unused types ([703ff45](https://github.com/choiceoh/Deneb/commit/703ff45e9e213a19bccdee366973ea2be80c8dd1))
* **ffi,session:** cap grow buffer, guard merge input, raise vega buf, enforce state transitions ([81141fb](https://github.com/choiceoh/Deneb/commit/81141fb3dd18f9474de2c656223fddc29506837c))
* **ffi:** cap ffiCallWithGrow initialSize, guard merge input, raise vega buffer ([51e84b0](https://github.com/choiceoh/Deneb/commit/51e84b07365e80cf194053a4c90ca36c87dcaba8))
* **ffi:** eliminate double-free race in CGo Handle via sync.Once ([e0228c5](https://github.com/choiceoh/Deneb/commit/e0228c53ee0a0e5db4131cc920d46867b4471de1))
* **ffi:** guard DetectMIME slice index against out-of-bounds n ([f2cf3eb](https://github.com/choiceoh/Deneb/commit/f2cf3eb821d8e8b80a61386f384b3c2d7d9aeb01))
* resolve 8 critical bugs across gateway, aurora, channel, auth, and build ([db5cf61](https://github.com/choiceoh/Deneb/commit/db5cf6173e7653f7ca69e3c05582246fb9f9cfff))


### 🔧 Internal

* **chat:** replace 36 hand-written *ToolSchema() functions with YAML + codegen ([99bc92e](https://github.com/choiceoh/Deneb/commit/99bc92e8f4b800c7421037a3abf729091b5d2b34))
* **core-rs:** replace .unwrap() with ? and .expect() in test code (continued) ([dbe2440](https://github.com/choiceoh/Deneb/commit/dbe244077cfd275423cf04022052bd1888da25c1))
* **core-rs:** replace .unwrap() with ? and .expect() in test code (final) ([fe2899f](https://github.com/choiceoh/Deneb/commit/fe2899f8f952598ae746cd58a2bfb1c0bf5e732a))
* **core-rs:** replace .unwrap() with ? and .expect() in test code (partial) ([37228f0](https://github.com/choiceoh/Deneb/commit/37228f0525bd9309a51125356b95af235d844e74))
* extract server lifecycle methods to separate file ([a1e20ba](https://github.com/choiceoh/Deneb/commit/a1e20ba87bc1a2cfc2de20306968dca5a542b918))
* **server:** split server.go into http/ws/lifecycle files ([6163624](https://github.com/choiceoh/Deneb/commit/6163624789117eb769a372d7ad42ae61cc5f178c))
* **session:** use ValidateTransition in Set, tighten comments ([a13282c](https://github.com/choiceoh/Deneb/commit/a13282c5d13c90c06925bd5ca9fe9499a9b0274e))

## [3.27.2](https://github.com/choiceoh/Deneb/compare/deneb-v3.27.1...deneb-v3.27.2) (2026-03-28)


### 🔧 Internal

* **core:** apply clippy lint fixes across Rust core crate ([41ae1a5](https://github.com/choiceoh/Deneb/commit/41ae1a5c4c43b359ab806f1a0d34d6365713bc42))
* **ffi:** extract FFI utilities into ffi_utils.rs ([1e48c08](https://github.com/choiceoh/Deneb/commit/1e48c08931455e98d44c736a59279fa23b7c33c9))
* **memory:** externalize stop word lists into stop_words module ([3e5bf1a](https://github.com/choiceoh/Deneb/commit/3e5bf1a82eeee7a7f43cbae3f919463d879f0592))
* **memory:** extract SIMD backends into simd/ submodule ([6d2a1cc](https://github.com/choiceoh/Deneb/commit/6d2a1cc88f5fc19e03f19f4f1c893c52dc420441))
* **scope:** split resolve.rs into agent_ids, session_keys, config modules ([593568b](https://github.com/choiceoh/Deneb/commit/593568b9f6fb544ffb5f851dc2313e63921c763b))
* **vega:** extract search, show, system commands to own files (WIP) ([a4056e1](https://github.com/choiceoh/Deneb/commit/a4056e1caddca2ba2dff82ae9e7d86338cc8b718))
* **vega:** trait-based CommandHandler registry, extract inline commands ([969dce7](https://github.com/choiceoh/Deneb/commit/969dce73742317073b2c05be25e867620a0b2cdc))
* **vega:** trait-based CommandHandler registry, extract inline commands ([e63e927](https://github.com/choiceoh/Deneb/commit/e63e9276dd398ef136a17d04735554726ea78a8b))

## [3.27.1](https://github.com/choiceoh/Deneb/compare/deneb-v3.27.0...deneb-v3.27.1) (2026-03-28)


### 🔧 Internal

* **chat:** reorganize coding tools into dedicated Code category ([0649fa8](https://github.com/choiceoh/Deneb/commit/0649fa844bf5ea79fca447323b47f2bf826ea1dc))

## [3.27.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.26.0...deneb-v3.27.0) (2026-03-28)


### ✨ Features

* **polaris:** add guide categorization, nodes guide, and cross-references ([3dec4f0](https://github.com/choiceoh/Deneb/commit/3dec4f0afc3b40edf909dfea947da72276ded4a2))


### 🐛 Bug Fixes

* **core:** use ? operator for option returns in html_to_markdown ([d479993](https://github.com/choiceoh/Deneb/commit/d47999351c666f0668212c77903c194083730598))
* **monitoring:** remove stale-activity watchdog check that kills gateway after 30min idle ([9415597](https://github.com/choiceoh/Deneb/commit/94155973f22a3b419fc7b859553fc8df44a040ec))


### 🔧 Internal

* **core:** extract FFI layer into ffi/ module and split stop words ([f3e71c0](https://github.com/choiceoh/Deneb/commit/f3e71c000b52755bd4d0ed67060dbd5542648c90))
* **polaris:** AI 에이전트 실용성 개선 ([0090a1f](https://github.com/choiceoh/Deneb/commit/0090a1fa7b680feced56ee9b3067c3e4c919b26e))
* **polaris:** rename files, add Common Tasks/Gotchas to all guides ([eccdac6](https://github.com/choiceoh/Deneb/commit/eccdac6485e9f82a856fda159605daaeb3e4c0b3))

## [3.26.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.25.0...deneb-v3.26.0) (2026-03-28)


### ✨ Features

* **pilot:** add diff, test, tree, git_log, health shortcuts and improve agent guidance ([1c95bd2](https://github.com/choiceoh/Deneb/commit/1c95bd2845dbfd19725acab90fc9599ad65aa54e))


### 🐛 Bug Fixes

* **gateway:** inject version via ldflags and filter unsupported channels from banner ([b3a4e03](https://github.com/choiceoh/Deneb/commit/b3a4e035495fa92a0f348193b0bbf34efe432138))
* **gateway:** remove auth from banner, add sglang status ([b50d5d9](https://github.com/choiceoh/Deneb/commit/b50d5d967f136d262ea1c3c07e776d1778dbad9c))
* **gateway:** use git tag version and remove channels from banner ([c066976](https://github.com/choiceoh/Deneb/commit/c0669760a8363acb1c3ce02a1f38ca056e3cdfdb))


### 🔧 Internal

* **autonomous:** remove goal system and autonomous tool, keep dreaming lifecycle ([2bf7047](https://github.com/choiceoh/Deneb/commit/2bf70474b619c8236101a09ad7861841b5303f20))
* **chat:** remove 5 redundant tools (ls, apply_patch, memory_get, session_status, sessions_restore) ([384cfc0](https://github.com/choiceoh/Deneb/commit/384cfc08f3533a96581604240183919d88e6417d))
* **core-rs:** eliminate all production unwrap() calls, enforce clippy::unwrap_used deny ([e89ed32](https://github.com/choiceoh/Deneb/commit/e89ed32c531c29f753cd3fed5389608c794ecb48))
* **gateway:** complete tools_pilot split and add hooks_http_exec ([c7e2cfa](https://github.com/choiceoh/Deneb/commit/c7e2cfa4bbb182b2fed92ea56649fa87e1194ce8))
* **gateway:** split large files in chat package for readability ([e4caa0e](https://github.com/choiceoh/Deneb/commit/e4caa0eda2081b98d834dfb54c2d79e60b1e2141))
* **gateway:** split server.go, process.go, and move cron stub ([9e52c92](https://github.com/choiceoh/Deneb/commit/9e52c929d78faf7328f690906d25189589a83f1f))
* **gateway:** split tools_pilot, tool_stubs, hooks_http, tool_manual ([721ab55](https://github.com/choiceoh/Deneb/commit/721ab55a867f965e11a76a9c752c0caf7a97fe3f))

## [3.25.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.24.1...deneb-v3.25.0) (2026-03-28)


### ✨ Features

* **chat:** change default model to google/gemini-3.0-flash ([f52d472](https://github.com/choiceoh/Deneb/commit/f52d47227fae280d8a866c335ee37918f17ff55f))


### 🐛 Bug Fixes

* **core:** prevent panic in html_to_markdown FFI ([9d03879](https://github.com/choiceoh/Deneb/commit/9d03879908219ce26a8f33837bdee9845d88f67e))
* **memory:** add per-phase timeouts to AuroraDream dreaming cycle ([aea4893](https://github.com/choiceoh/Deneb/commit/aea48935c57e3bae30298cf6b823504ac2b4afea))
* **memory:** raise dreaming cycle ceiling to 12 minutes ([b56b6ce](https://github.com/choiceoh/Deneb/commit/b56b6ce2b0cf9c0b1ac0e0d6362a07117d0b6145))
* **memory:** raise per-phase dreaming budgets to ~15 minutes total ([8e1543f](https://github.com/choiceoh/Deneb/commit/8e1543fa2d915a64d077fb34a7f72a14086429e7))
* **telegram:** add media group batching, download timeout, and error handling for image processing ([b58139c](https://github.com/choiceoh/Deneb/commit/b58139cd6f8ebe1f006557199073040a6cea81a4))


### ⚡ Performance

* **memory:** improve aurora memory recall with higher limits, rebalanced scoring, and embedding cache ([dbe332a](https://github.com/choiceoh/Deneb/commit/dbe332acc99c05c80bffa21809b5d1a058d1550e))
* **memory:** increase verify batch size to 50 and cap facts at 500 ([a228287](https://github.com/choiceoh/Deneb/commit/a228287e7be9deadac38e0f8cff24de70ca5c025))


### 🔧 Internal

* **gateway:** remove copilot background monitor and github-copilot xhigh support ([74d96b6](https://github.com/choiceoh/Deneb/commit/74d96b68c934824b8d57cbf94d7dc41ace0f7d48))
* remove dead code across core-rs and gateway-go ([3d9b9be](https://github.com/choiceoh/Deneb/commit/3d9b9be00daa07ee73b09393d47bcaae1ad8665b))
* remove Propus channel (web coding assistant) ([4df5cc7](https://github.com/choiceoh/Deneb/commit/4df5cc7c48e18a112847d07c63dfb76829100afc))
* remove Propus-only hooks from chat handler (GetReplyFunc, GetBroadcastRaw, ToolProfile, codingTools) ([6ad3550](https://github.com/choiceoh/Deneb/commit/6ad3550292b634bf4620c05e6284be6d1bb825fa))

## [3.24.1](https://github.com/choiceoh/Deneb/compare/deneb-v3.24.0...deneb-v3.24.1) (2026-03-28)


### 🔧 Internal

* **propus:** convert Tauri desktop app to web SPA ([f153287](https://github.com/choiceoh/Deneb/commit/f1532870051f4d8b09d09df536344b318887702d))

## [3.24.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.23.0...deneb-v3.24.0) (2026-03-28)


### ✨ Features

* **propus:** add session history browser with search and session switching ([c081893](https://github.com/choiceoh/Deneb/commit/c08189372e090925b8a497700a9a39c0abb44376))


### 🐛 Bug Fixes

* **chat:** add StripTrailingCommas to UnmarshalLLM pipeline ([7ba1d92](https://github.com/choiceoh/Deneb/commit/7ba1d926baabfd5395635184ae509d31078d0500))
* **chat:** fix ExtractObject trailing prose bug, harden extraction ([356e77f](https://github.com/choiceoh/Deneb/commit/356e77f2798c257db3e43fd684b431b8074084da))
* **memory:** remove data volume trigger from dreaming to prevent infinite re-trigger loop ([2f77d80](https://github.com/choiceoh/Deneb/commit/2f77d80a4d9c890feca789aad8d17b0671152922))


### ⚡ Performance

* **chat:** replace regex with scanner, unify bracket-depth tracking ([431750c](https://github.com/choiceoh/Deneb/commit/431750c9f4c1a693217e1e4af7dc0ef1db47e558))


### 🔧 Internal

* **chat:** adopt jsonutil.UnmarshalInto in remaining tool handlers (batch 2) ([77d8475](https://github.com/choiceoh/Deneb/commit/77d84752b4a798feedc0de78b16ba5928beee0c5))
* **chat:** adopt jsonutil.UnmarshalInto in tool handlers (batch 1) ([9822588](https://github.com/choiceoh/Deneb/commit/98225888e023565dc4da5a0c3c225cd3fb1fb770))
* **chat:** extract shared JSON parsing into pkg/jsonutil ([c24ade5](https://github.com/choiceoh/Deneb/commit/c24ade59113bd9c1dcd3b840d68eaff367972144))

## [3.23.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.22.0...deneb-v3.23.0) (2026-03-28)


### ✨ Features

* **propus:** add auto-updater plugin + update button in sidebar ([5c9f045](https://github.com/choiceoh/Deneb/commit/5c9f045117661922a132df2de030f85a2460c079))
* **propus:** add markdown rendering, keyboard shortcuts, session resume, tool display improvements, performance optimization, and image preview ([a907f10](https://github.com/choiceoh/Deneb/commit/a907f101c37f2db4dc310a36258d73e857e12e30))
* **propus:** add Windows icon for Tauri build ([148b0d9](https://github.com/choiceoh/Deneb/commit/148b0d93c4bc5aefb884e21c8be91a5d5d2ef1cd))
* **propus:** wire Config.Tools to toolProfile, send real model in ConfigStatus, add File/Typing message handling and code block copy button ([0bca78d](https://github.com/choiceoh/Deneb/commit/0bca78de7d2ec46f11ba1e42cb0b22a514315165))


### 🐛 Bug Fixes

* **chat:** prevent agent from asking user to do tool-capable actions ([a4180aa](https://github.com/choiceoh/Deneb/commit/a4180aa0339317499d41c5001883311eed516133))
* **chat:** prevent agent from asking user to do tool-capable actions ([3a69bc2](https://github.com/choiceoh/Deneb/commit/3a69bc2491f8a19f301d8524a5b03889811de1ff))
* **core:** handle multi-byte UTF-8 in html_to_markdown ([4484cd4](https://github.com/choiceoh/Deneb/commit/4484cd4520f3b37fdd0a690b1563790d2ed3f2cc))
* **core:** strip trailing punctuation from extracted URLs ([fd4025c](https://github.com/choiceoh/Deneb/commit/fd4025c180d14ce5f5b358248bc17eb2021d0399))
* **llm:** strengthen retry logic for 429 rate limits ([c7ebb3b](https://github.com/choiceoh/Deneb/commit/c7ebb3b4381b702e7f78fee6b398dc10b28c82d2))
* **propus:** add bundle config with empty icon to fix Windows build ([c53cf12](https://github.com/choiceoh/Deneb/commit/c53cf12b517a57636903287e06ee8b9c8e16f8f4))
* **propus:** add main.rs and remove duplicate fn main from lib.rs ([5fb1f24](https://github.com/choiceoh/Deneb/commit/5fb1f24ffe65e26c33aa11bb6e589d514021396c))
* **propus:** add main.rs binary entry point ([d1d10ea](https://github.com/choiceoh/Deneb/commit/d1d10ea3852ccc4be05dbf3e2f6b8bfb509b7506))
* **propus:** add standalone workspace to avoid root workspace conflict ([204de2d](https://github.com/choiceoh/Deneb/commit/204de2d837f38487f2e32804a95288e77ba1ecf1))
* **propus:** exclude from root workspace, add standalone workspace members ([242b5a3](https://github.com/choiceoh/Deneb/commit/242b5a3938df6b8499a6c2b5c4078369bf3ff80c))
* **propus:** remove invalid app.title from tauri.conf.json for Tauri 2.0 compat ([0223640](https://github.com/choiceoh/Deneb/commit/02236406b84453761133de521422965310a8a93f))
* **propus:** restore lib.rs with updater commands, remove fn main ([be84779](https://github.com/choiceoh/Deneb/commit/be847795636e6b3ba5a1c2f1012ffcc4951101b0))

## [3.22.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.21.1...deneb-v3.22.0) (2026-03-28)


### ✨ Features

* add autonomous tool to agent tool registry ([903755f](https://github.com/choiceoh/Deneb/commit/903755f794dc196ff218b2e64e286188d16fabea))
* add autonomous tool to system prompt descriptions and tool order ([88871ec](https://github.com/choiceoh/Deneb/commit/88871ec29b2e7e24e4a7cd30379c73c934f53000))
* add cargo-deny config, DuckDB analytics scripts, and Makefile targets ([9793986](https://github.com/choiceoh/Deneb/commit/9793986c25b600ad38ebcf1a2b22ca9cafb26b5d))
* add edited_message, edited_channel_post, my_chat_member handlers and narrow allowed-updates ([#314](https://github.com/choiceoh/Deneb/issues/314)) ([4950861](https://github.com/choiceoh/Deneb/commit/49508613314006e5d84416c411bd55827780e5e8))
* add gmail tool usage guide to system prompt tool selection ([39c24d2](https://github.com/choiceoh/Deneb/commit/39c24d222806f96a1b5fc0ec7259df88871bd99a))
* add gmail tool with inbox summary, search, send, reply, labels and contact aliases ([4cf9158](https://github.com/choiceoh/Deneb/commit/4cf91582864cbf7037fcf0f032d9196cb483578b))
* add health check, thinking mode, chaining, smart truncation, and metrics ([75061a7](https://github.com/choiceoh/Deneb/commit/75061a70cd0df8e2a7429e322cbdb0334987c79b))
* add Honcho-inspired structured memory system with SGLang ([8b37aa2](https://github.com/choiceoh/Deneb/commit/8b37aa2b7e36cc9a3be4437db12e5bbae11e84e3))
* add IPv4 fallback transport and retry to client ([#282](https://github.com/choiceoh/Deneb/issues/282)) ([0bf27d0](https://github.com/choiceoh/Deneb/commit/0bf27d02c06ee9de6cc2ab4a323af577cde5ca17))
* add JSON response cleaning for output_format json ([6bd62be](https://github.com/choiceoh/Deneb/commit/6bd62be5f769d8d5269b237400c4810d623b4cdb))
* add LiteParse integration for document parsing ([a716a66](https://github.com/choiceoh/Deneb/commit/a716a664f44d1f2399bce6ff1b28b47e66f9a44f))
* add mutual understanding tracking to Aurora Dream ([b6c7de9](https://github.com/choiceoh/Deneb/commit/b6c7de9faa965cf960061d082d319884b71af411))
* add output post-processing — markdown normalization, list cleaning, length enforcement ([f5d3cf6](https://github.com/choiceoh/Deneb/commit/f5d3cf6ed894e0411d330665d4b40fbd72e763c4))
* add send_file, http, kv, clipboard agent tools ([#399](https://github.com/choiceoh/Deneb/issues/399)) ([37c6645](https://github.com/choiceoh/Deneb/commit/37c6645f19622ee6c214cef610de25ab4e608c6a))
* add sglang fallback and local summarization ([#398](https://github.com/choiceoh/Deneb/issues/398)) ([9b48bf8](https://github.com/choiceoh/Deneb/commit/9b48bf8abf195958e355c1ff45d653c6ee8380ed))
* add SOUL.md activation instruction to system prompt ([#350](https://github.com/choiceoh/Deneb/issues/350)) ([5284377](https://github.com/choiceoh/Deneb/commit/52843778c9415420c0ae5b502f5880a1057e6c2a))
* add status reaction emoji on user message during agent runs ([3351488](https://github.com/choiceoh/Deneb/commit/3351488d2d241bb456179c9469fe7bf41734326b))
* add system_manual tool for queryable Deneb documentation ([60eff64](https://github.com/choiceoh/Deneb/commit/60eff64d798cab3a4adc0e279eafb01cd239f6b1))
* **aurora:** add Aurora desktop RPC channel handlers ([d2efd4e](https://github.com/choiceoh/Deneb/commit/d2efd4e12308fe1cbb6c3a12016d1f68470f98eb))
* **aurora:** Aurora 데스크톱 RPC 채널 핸들러 ([467cff3](https://github.com/choiceoh/Deneb/commit/467cff3ce07811dd5838569152550957c47ee87a))
* auto-detect embedding server on DGX Spark startup ([affd107](https://github.com/choiceoh/Deneb/commit/affd107512bea1ad75993eff3bf0558045992ba0))
* auto-launch SGLang embedding server on DGX Spark ([08620fe](https://github.com/choiceoh/Deneb/commit/08620fe4641780790eb2d4a9120a50a48439941a))
* **autonomous:** auto-set goals from recalled memory facts during knowledge prefetch ([97a285a](https://github.com/choiceoh/Deneb/commit/97a285a8f5a166bdadc74d537942ad6c16a62bdd))
* **chat:** add agent detail log system for AI self-diagnostics ([15c25b0](https://github.com/choiceoh/Deneb/commit/15c25b0e1095c62f1b9de7420389ed6443f19b8a))
* **chat:** add agent detail log system for AI self-diagnostics ([7bce087](https://github.com/choiceoh/Deneb/commit/7bce0875d5ab813b6756291580d079b509aaa334))
* **chat:** add git, analyze, test tools and improve existing tools ([f26880d](https://github.com/choiceoh/Deneb/commit/f26880d28646a61a9836574d978aaef631681662))
* **chat:** add git, analyze, test tools and improve existing tools ([e76314c](https://github.com/choiceoh/Deneb/commit/e76314c1e8136bbdb715c398d3845fb4cddc138c))
* **chat:** add multi_edit, tree, diff coding tools ([4e1f9dd](https://github.com/choiceoh/Deneb/commit/4e1f9dde35896982b8978100cec40c384ec2eb4e))
* **chat:** add natural emoji guidance to response style ([bbb3f8a](https://github.com/choiceoh/Deneb/commit/bbb3f8ac8ce36fe5767d77d13d3126fdc2bba456))
* **chat:** add temporal context awareness to memory fact display ([bfe14a5](https://github.com/choiceoh/Deneb/commit/bfe14a50bd04c5099325336cdc5e5c5b761ea478))
* **chat:** make agent_logs pilot-only with shortcut and system prompt guidance ([21d434e](https://github.com/choiceoh/Deneb/commit/21d434e07fbba278465ec83186733051ffd326ce))
* **chat:** update default model from glm-5-turbo to glm-5.1 ([a13aee5](https://github.com/choiceoh/Deneb/commit/a13aee5920f3f8f416bfb16df3fd1eb9b4323a5f))
* **chat:** update default model to glm-5.1 ([29d41da](https://github.com/choiceoh/Deneb/commit/29d41da62cfedb1dbccbe693b956e100896c769d))
* **cli:** refine terminal design with Apple aesthetic philosophy ([6f445d8](https://github.com/choiceoh/Deneb/commit/6f445d85381710a5dc506eca31adb104b1a41f07))
* compaction + quality filtering for MEMORY.md ([ab06a40](https://github.com/choiceoh/Deneb/commit/ab06a40372e0b833b8d96b71be339d63cee00316))
* complete Python-to-Rust migration for Vega ([#304](https://github.com/choiceoh/Deneb/issues/304)) ([e93e541](https://github.com/choiceoh/Deneb/commit/e93e541fadb2d58e5d6dca58415156f425be2bc4))
* **cron:** add morning-letter skill and cron job for daily 8AM KST briefing ([4b9b6f5](https://github.com/choiceoh/Deneb/commit/4b9b6f575a549a616ce946a6cf6ee46ce4542ae8))
* deep improvements — gateway init, dedup, migration, conflict resolution, Korean FTS, mid-run extraction, Neuromancer-style prompts ([fb317cd](https://github.com/choiceoh/Deneb/commit/fb317cdb188221c2c1fedf1bb34182b7543cd256))
* deepen mutual understanding tracking ([347529a](https://github.com/choiceoh/Deneb/commit/347529ae0303fb6d0ac2ef04a535c82058c42c25))
* deepen mutual understanding with real-time signals, history, and cross-phase integration ([e97e0c1](https://github.com/choiceoh/Deneb/commit/e97e0c1e271375492cac868601d1390a55a98488))
* downgrade context canceled polling error to info level ([#351](https://github.com/choiceoh/Deneb/issues/351)) ([90bba33](https://github.com/choiceoh/Deneb/commit/90bba3368dc53111d25381eca8fef0bd18dd6246))
* enhance inter-tool integration ([08c061f](https://github.com/choiceoh/Deneb/commit/08c061fe778ee595c94f603889f041fedf8ff62b))
* enhance tool schemas with enum/default constraints and improve system prompt guidance ([#403](https://github.com/choiceoh/Deneb/issues/403)) ([99e9150](https://github.com/choiceoh/Deneb/commit/99e91504f456e5ac2afc708810633e08b979a55c))
* enrich tool descriptions in system prompt ([47f70c7](https://github.com/choiceoh/Deneb/commit/47f70c7e0be0eca1ff174387768f8c126f76a99a))
* fix bugs, deadlock risks, and add comprehensive tests ([#382](https://github.com/choiceoh/Deneb/issues/382)) ([1a66bda](https://github.com/choiceoh/Deneb/commit/1a66bdae91b4e93f42498a5509caacfa6a3ca86f))
* fix code review issues ([9d7804d](https://github.com/choiceoh/Deneb/commit/9d7804d4b3c33c7b43b3d0c6b92eea48e9fdf915))
* fix config schema, add reaction types, upload retry, tighten buffer ([#285](https://github.com/choiceoh/Deneb/issues/285)) ([ad2fdf5](https://github.com/choiceoh/Deneb/commit/ad2fdf5ee52ca2a3a92a5a09069f91b099c817e0))
* fix deadlock, race condition, and deduplicate stream helpers ([276146c](https://github.com/choiceoh/Deneb/commit/276146c32bcf188f82191dcbd8c1d3665c10f97c))
* fix HTTP client timeout shorter than long-poll timeout ([#309](https://github.com/choiceoh/Deneb/issues/309)) ([70aed49](https://github.com/choiceoh/Deneb/commit/70aed4974c04ab069f609b0782ae848a113b8917))
* fix production issues — transcript reset, error tracking, enabled persistence, initial cycle, Telegram notifications ([#395](https://github.com/choiceoh/Deneb/issues/395)) ([dba1f21](https://github.com/choiceoh/Deneb/commit/dba1f2114cd5e3f084d55a6297085a6886946d72))
* fix review issues — 2 bugs, 4 logic, 2 prompt, 1 style ([dab41be](https://github.com/choiceoh/Deneb/commit/dab41be151ea2ba7980576047ad72113146a9f31))
* fix review issues — UTF-8 truncation, Anthropic path, short message filter ([d0014f2](https://github.com/choiceoh/Deneb/commit/d0014f2092f7efb7d97711f80ba8de493ca11a89))
* fix second review — UTF-8 safety, sql.ErrNoRows, signal cleanup, tests ([f4755cc](https://github.com/choiceoh/Deneb/commit/f4755cc468052d72413cd03f12379cfac0affb00))
* fix silent message drops from inbound debounce ([#228](https://github.com/choiceoh/Deneb/issues/228)) ([92a7414](https://github.com/choiceoh/Deneb/commit/92a7414a7d95205d761fdc55ea6eb3e7472ee3b1))
* fix status reactions with Telegram-compatible emojis and error logging ([41a5099](https://github.com/choiceoh/Deneb/commit/41a50998c9b4b5ecd212e59c3f8d90ca48eada72))
* fix truncation overlap panic, brief+thinking conflict, chain self-call guard ([35f7f8e](https://github.com/choiceoh/Deneb/commit/35f7f8e321b70e9b7b58db5e94a873722ebca330))
* fix updateUserModelFromFact reading from wrong table ([9a51dae](https://github.com/choiceoh/Deneb/commit/9a51dae03598fd690e6ce87ce3435270048ac767))
* **gmail:** add periodic Gmail polling with LLM analysis ([dd64ccb](https://github.com/choiceoh/Deneb/commit/dd64ccbac21b180e72aaa2fce9e763ddd395a458))
* **go:** add property-based and benchmark tests for session and RPC ([bee0d07](https://github.com/choiceoh/Deneb/commit/bee0d077e6f7b8d0f89e865c010f8cc90308480d))
* **go:** add stdlib metrics package with Prometheus-compatible /metrics endpoint ([dd9861d](https://github.com/choiceoh/Deneb/commit/dd9861d4de54628e8da1763a275219a0e31dd280))
* implement Go host-side orchestration for Rust compaction engine ([#354](https://github.com/choiceoh/Deneb/issues/354)) ([8406e00](https://github.com/choiceoh/Deneb/commit/8406e00c19e5be4ecba455cb8680e23df60c073f))
* implement subagents tool with session manager integration ([#364](https://github.com/choiceoh/Deneb/issues/364)) ([ea085d2](https://github.com/choiceoh/Deneb/commit/ea085d23ce73f943effa80d2aa9dcb6131e248a7))
* improve agent tool usage, response speed, and action efficiency ([e82df17](https://github.com/choiceoh/Deneb/commit/e82df1736b04075a33943bea0c9e1beaef3876c0))
* improve cache utilization across hot paths ([54e1d4a](https://github.com/choiceoh/Deneb/commit/54e1d4a7045c3c9f105a03ff19806b3c03dc8735))
* include workspace directory in sglang system prompt ([#409](https://github.com/choiceoh/Deneb/issues/409)) ([046cd6d](https://github.com/choiceoh/Deneb/commit/046cd6d84bf36f5c5f0779910edcd1593d99ba72))
* integrate dreaming into autonomous service lifecycle ([0fb9cb1](https://github.com/choiceoh/Deneb/commit/0fb9cb157612a31c67ebd70148d5fea942ad4e9b))
* integrate Vega + Memory knowledge prefetch into context assembly ([b531601](https://github.com/choiceoh/Deneb/commit/b531601e369c91ab0b32ffe309e4bf54e6309a68))
* **memory:** add category-aware importance, recency, and frequency weighting ([4437e71](https://github.com/choiceoh/Deneb/commit/4437e71c0d84f87bc94f36240e282845f28bde34))
* **memory:** add data volume trigger for dreaming and fix turn increment bug ([fc8d407](https://github.com/choiceoh/Deneb/commit/fc8d407abb00c264711040ccbfe57931551e2a76))
* **memory:** add data volume trigger for dreaming and fix turn increment bug ([538d5c6](https://github.com/choiceoh/Deneb/commit/538d5c64b5c84d2797870d65f868d99fe745ed65))
* optimize AI agent tools for parallel execution and richer schemas ([#400](https://github.com/choiceoh/Deneb/issues/400)) ([b616bd6](https://github.com/choiceoh/Deneb/commit/b616bd65ac62839d917ef067cff0a218b2a63a56))
* optimize inbound pipeline latency (async handler, reduced timeouts) ([f0c6d3c](https://github.com/choiceoh/Deneb/commit/f0c6d3cfd96c7057b41a4016493b6db26ab965ea))
* parallelize knowledge+context prep, add pipeline timing, reduce proactive timeout ([7cfec95](https://github.com/choiceoh/Deneb/commit/7cfec95645d34fd2dd0c4eb83093a60de13a9dae))
* **pilot:** add gateway_logs tool for querying gateway process logs ([9f96cd6](https://github.com/choiceoh/Deneb/commit/9f96cd61bc5e3fb590cff68057b5cbc9a0857c73))
* **pilot:** add gateway_logs tool for querying gateway process logs ([e8ef254](https://github.com/choiceoh/Deneb/commit/e8ef2545fba9c3be531d3144feb830d230c381a1))
* **pilot:** add shortcuts for gmail, youtube, polaris, image, clipboard, ls, vega and register vega chat tool ([7f17030](https://github.com/choiceoh/Deneb/commit/7f170300d3cb7e9017158bb31221095d770eefc7))
* **polaris:** add 5 new guides and update existing guide content ([ee55306](https://github.com/choiceoh/Deneb/commit/ee553069f263677722768d4c84798fe614da8a8d))
* **polaris:** add FFI bridge, RPC, auth details to architecture guide ([19d5dcf](https://github.com/choiceoh/Deneb/commit/19d5dcf18c1ec65c47df74e7e8c9075c431746fc))
* **polaris:** enrich 15 existing guides and add 8 new tool guides ([3124ed7](https://github.com/choiceoh/Deneb/commit/3124ed7af32d293160c0e06822a332109054cebe))
* **polaris:** improve manual with compact topics, better search, new guides ([31ecdc5](https://github.com/choiceoh/Deneb/commit/31ecdc58097e96b7da438f5d47e42dc7a149922f))
* port 11 OpenClaw 3.22/3.23 features (security, performance, Telegram) ([#294](https://github.com/choiceoh/Deneb/issues/294)) ([9ab2fe6](https://github.com/choiceoh/Deneb/commit/9ab2fe6976238aab43bccd0035786a2de4fbfa15))
* port missing Python features to Rust (E-2 through E-7) ([#323](https://github.com/choiceoh/Deneb/issues/323)) ([b6a6ee9](https://github.com/choiceoh/Deneb/commit/b6a6ee9d6801ed1fc37fd76807d12642f9d56e67))
* **propus:** add auto-updater plugin + update button in sidebar ([5c9f045](https://github.com/choiceoh/Deneb/commit/5c9f045117661922a132df2de030f85a2460c079))
* **propus:** add Propus client main.rs ([ba00245](https://github.com/choiceoh/Deneb/commit/ba00245b859697162407c66a343fa8a703bd0008))
* **propus:** add Propus client source and Go channel plugin ([c84c22d](https://github.com/choiceoh/Deneb/commit/c84c22dab66a5ae94d06fac79b8460d5aa2b8938))
* **propus:** add Propus desktop coding channel ([c95c30d](https://github.com/choiceoh/Deneb/commit/c95c30d116047d395d1f37df3552e51e272c3570))
* **propus:** add Propus Slint UI (app.slint) ([5e5f7cc](https://github.com/choiceoh/Deneb/commit/5e5f7cce27e649d9752aa62ef24bb17bd96e1d62))
* **propus:** enhance to production level with session save, file delivery, heartbeat, typing, and tests ([475039d](https://github.com/choiceoh/Deneb/commit/475039d1aeaf59742ec123c489901a328079b5ca))
* **propus:** wire Config.Tools to toolProfile, send real model in ConfigStatus, add File/Typing message handling and code block copy button ([0bca78d](https://github.com/choiceoh/Deneb/commit/0bca78de7d2ec46f11ba1e42cb0b22a514315165))
* **propus:** wire Propus channel registration, chat.send pipeline, and streaming events ([a3bf5d5](https://github.com/choiceoh/Deneb/commit/a3bf5d54113165fd0a52815a9f45f7a95d763399))
* **propus:** wire tool event broadcasting and coding tool profile ([c437ce8](https://github.com/choiceoh/Deneb/commit/c437ce8d66a50ec8ba41083fae8267b4c5547e36))
* reduce compress timeout (30s→10s) and raise threshold (8K→16K) ([e27790d](https://github.com/choiceoh/Deneb/commit/e27790d86207a797c5abedc141cd45b761ad6932))
* remove redundant and low-value tests to reduce test time ([#220](https://github.com/choiceoh/Deneb/issues/220)) ([1c18180](https://github.com/choiceoh/Deneb/commit/1c181808af863364fc7c813da9c261c3387f1ce6))
* rename system_manual tool to polaris ([815d438](https://github.com/choiceoh/Deneb/commit/815d4383b447d2c4df1aadf90125b91500e997d1))
* replace GGUF models with SGLang for embedding and query expansion ([a15721e](https://github.com/choiceoh/Deneb/commit/a15721e6c61adf788c74293b6d95aaa23847ead0))
* replace simple typing ticker with phase-aware FullTypingSignaler ([def5a92](https://github.com/choiceoh/Deneb/commit/def5a927b4b38fd76cd934e58cb963a16bdb1fd1))
* **rust:** add proptest property tests for protocol frames and cosine similarity ([2261202](https://github.com/choiceoh/Deneb/commit/22612028307ed571158339519fd764e4f509c316))
* send typing indicator during agent runs ([9b91fda](https://github.com/choiceoh/Deneb/commit/9b91fdaa4dc010bed0143ef9535e7d99b13193fa))
* store handler on Plugin so it survives until bot Start ([#310](https://github.com/choiceoh/Deneb/issues/310)) ([3a8fa10](https://github.com/choiceoh/Deneb/commit/3a8fa107aa315a75dbd6579f163e6dc8a3f1d171))
* use detached context for polling goroutine ([#305](https://github.com/choiceoh/Deneb/issues/305)) ([ca80cac](https://github.com/choiceoh/Deneb/commit/ca80cac862e3f81d57ff4cb28530c68555708162))
* use MarkdownToTelegramHTML for reply formatting (fixes fenced code blocks) ([5db3d35](https://github.com/choiceoh/Deneb/commit/5db3d35fc8949bee1cc82214b6e879fd1d7b3de5))
* **vega:** add aurora-memory and autonomous health checks to health_check tool ([d9b9cbf](https://github.com/choiceoh/Deneb/commit/d9b9cbfa6ee7c1209071bd8e0317e595c146f1a1))
* **vega:** add health_check tool for embedding, reranker, and sglang diagnostics ([74f4ee2](https://github.com/choiceoh/Deneb/commit/74f4ee23d60b45790341c63e5d1ff8220963951a))
* **vega:** add health_check tool for embedding, reranker, and sglang diagnostics ([fe0ea4f](https://github.com/choiceoh/Deneb/commit/fe0ea4fd36c655b5ef5e01fb51bc8d9303ed8fa7))
* **vega:** add jina-reranker-v2 cross-encoder reranking to search and memory pipelines ([8f39925](https://github.com/choiceoh/Deneb/commit/8f399257aa72ba867ea8d95df7ec3a38daa617e6))
* **vega:** implement command registry, search router, and SQLite FTS engine ([#272](https://github.com/choiceoh/Deneb/issues/272)) ([a85d431](https://github.com/choiceoh/Deneb/commit/a85d431beca53c003817b731751d583f166a134a))
* **vega:** replace local SGLang embedder with Gemini Embedding API (gemini-embedding-2-preview) ([13403b5](https://github.com/choiceoh/Deneb/commit/13403b52ea5276d2233d9c78766b1bc6c66a0637))
* wire StartPeriodicTimer at gateway init ([0b9aad0](https://github.com/choiceoh/Deneb/commit/0b9aad0eb54177ea42f7c02bba5c4e013ed0be89))


### 🐛 Bug Fixes

* add module prefixes to release-please changelog-sections ([e7471e7](https://github.com/choiceoh/Deneb/commit/e7471e753aa012c8fc784651e14d6ed9f4a4d852))
* agents.defaults.model parsing + memory_search diagnostics ([#342](https://github.com/choiceoh/Deneb/issues/342)) ([c08f09e](https://github.com/choiceoh/Deneb/commit/c08f09e5db3cf4a7c86d02f86ae25ed942815cdd))
* **aurora:** pass Gemini embedder and Jina key as server options to fix init ordering ([5e414e6](https://github.com/choiceoh/Deneb/commit/5e414e69e73218afbfeb51b6583ff0609f3da9c2))
* **aurora:** pass Gemini embedder and Jina key as server options to fix init ordering ([7031b1f](https://github.com/choiceoh/Deneb/commit/7031b1fc6fa69fa29546f2ada9b6fa8da129a375))
* **aurora:** robust JSON extraction for dream cycle LLM responses ([38422ce](https://github.com/choiceoh/Deneb/commit/38422ce681285061b4d18d86dce0c04943d3dd11))
* **autonomous:** replace lowest-priority goal instead of rejecting when limit reached ([6952adc](https://github.com/choiceoh/Deneb/commit/6952adc0a0de9e69150aa2fff685f2eedcd43c38))
* **autonomous:** replace lowest-priority goal instead of rejecting when limit reached ([9ec4b50](https://github.com/choiceoh/Deneb/commit/9ec4b501f86ecc693dba08c0a7ecceaeda7f7014))
* **chat:** add missing RunLogger arg to executeAgentRun call in send_sync ([87f6af5](https://github.com/choiceoh/Deneb/commit/87f6af5ae780420e68a8310d6d4e2c457166f167))
* **chat:** improve temporal annotations with natural Korean and two-tier volatility ([3ca53ee](https://github.com/choiceoh/Deneb/commit/3ca53ee90b2651e97d9f21b62413bf4553aae4a0))
* **chat:** use cached sglang status instead of active health probe ([74a15d2](https://github.com/choiceoh/Deneb/commit/74a15d2d22a5342a1a9daa91c39a252ab1a1a3b4))
* clean up stale references and build artifacts from CLI removal ([#242](https://github.com/choiceoh/Deneb/issues/242)) ([e342d3c](https://github.com/choiceoh/Deneb/commit/e342d3cabddafe87e6de3ad8730e9435a32a87b7))
* **core:** handle multi-byte UTF-8 in html_to_markdown ([4484cd4](https://github.com/choiceoh/Deneb/commit/4484cd4520f3b37fdd0a690b1563790d2ed3f2cc))
* **core:** replace mutex unwrap with poison-recovery in NAPI FFI boundary ([c250eaf](https://github.com/choiceoh/Deneb/commit/c250eaf1b57f757912c7f1d848d6fa3fb17756b7))
* correct FFI error codes for session key validation and buffer-too-small returns ([#352](https://github.com/choiceoh/Deneb/issues/352)) ([8f882cc](https://github.com/choiceoh/Deneb/commit/8f882ccaffee5b0a8e722bf7146b3f3d326750bc))
* correct Rust base64 test assertion, Go ML test stub handling, and format drift ([#316](https://github.com/choiceoh/Deneb/issues/316)) ([19712ee](https://github.com/choiceoh/Deneb/commit/19712ee5a3e7cdb03feedc340735b36da48a3021))
* fix release-please config and sync all versions to 3.8.0 ([#254](https://github.com/choiceoh/Deneb/issues/254)) ([55f4696](https://github.com/choiceoh/Deneb/commit/55f4696b3e486f4b3c012aff650fd2f79f808e3f))
* **gateway-go:** fix Telegram chat handler bugs — unique request IDs, reply timeouts, strict channel filter ([#311](https://github.com/choiceoh/Deneb/issues/311)) ([3a96b01](https://github.com/choiceoh/Deneb/commit/3a96b0123850a9311adf0010cba80acf6f8c868f))
* **gateway:** access GmailPoll via ConfigSnapshot.Config field ([baab8a0](https://github.com/choiceoh/Deneb/commit/baab8a0d1e5c03bec7e054d05a89c55064f6ae80))
* **gateway:** remove stale embedResult reference in startup banner ([bec6c84](https://github.com/choiceoh/Deneb/commit/bec6c849bbf456fc8c54273db3b6366db7702ac8))
* **gateway:** rollback Go version from 1.25.0 to 1.24.7 ([0091bd9](https://github.com/choiceoh/Deneb/commit/0091bd90f3aed53a9f08be0bd6aaf67d4572fc64))
* handle agents.defaults.model as json.RawMessage (string or object) ([#339](https://github.com/choiceoh/Deneb/issues/339)) ([ad11af9](https://github.com/choiceoh/Deneb/commit/ad11af9fcd58abe4f175034020034cc0b90d3a91))
* harden Go/Rust FFI build — buffer growth, handle safety, error codes ([#298](https://github.com/choiceoh/Deneb/issues/298)) ([93c68a6](https://github.com/choiceoh/Deneb/commit/93c68a68281eb8a2f26151ac266f56c248c91bbb))
* link @deneb/native as file dependency and remove orphaned native/ ([#271](https://github.com/choiceoh/Deneb/issues/271)) ([95b2d09](https://github.com/choiceoh/Deneb/commit/95b2d0947f0f5aa6c889e3d80fb7d0cf126c4036))
* **memory:** add independent dreaming timer and fix silent failures in ShouldDream ([1c924d7](https://github.com/choiceoh/Deneb/commit/1c924d783a4580bcf6a834feb239f5a62f15e81a))
* **memory:** align importance JSON parsing with json_object response format ([abb8e32](https://github.com/choiceoh/Deneb/commit/abb8e3285d479a0458355a3e5c50b563ef12106f))
* **memory:** embed Phase 3 pattern facts for future dedup ([840ca6b](https://github.com/choiceoh/Deneb/commit/840ca6b3afd6b5604189f230d379f48167bd99d7))
* **memory:** enforce JSON mode and strip thinking tags in AuroraDream dreaming ([86527f5](https://github.com/choiceoh/Deneb/commit/86527f5a64d800d7fbf0c0399a5125125404c088))
* **memory:** improve aurora-dream conflict resolution accuracy ([014d069](https://github.com/choiceoh/Deneb/commit/014d0690f284826366da249f6c710d82a831ae0c))
* **memory:** increase importance maxTokens to 1536 and add truncated JSON recovery ([a0ddcdf](https://github.com/choiceoh/Deneb/commit/a0ddcdf70e343ab4c0eab352b5cc0f95b31d3684))
* **memory:** increase importance maxTokens to 1536 and add truncated JSON recovery ([0663f59](https://github.com/choiceoh/Deneb/commit/0663f59ae073b67e3f8ed3032c9d7a538b44bf9a))
* **memory:** prevent cascading fact merges in aurora-dream ([0e51696](https://github.com/choiceoh/Deneb/commit/0e51696204a075ea8e99adbfa87f7e2dc4f3c8fa))
* **memory:** prevent JSON parse failures in importance extraction ([3fb0f5b](https://github.com/choiceoh/Deneb/commit/3fb0f5b0c8f05f185827ae361c8e8a35800c8b5f))
* **memory:** prevent removed facts from being used as conflict winners ([b4e97b1](https://github.com/choiceoh/Deneb/commit/b4e97b1e1ccf49e90e991377f5ac9660dba89198))
* **memory:** retry importance fact extraction on parse failure ([1251025](https://github.com/choiceoh/Deneb/commit/125102518bbb5101cf3493b241af029c910cba5c))
* **memory:** strengthen Phase 2 merge to reduce false conflicts ([1ea54d8](https://github.com/choiceoh/Deneb/commit/1ea54d8cd52721bfde5bc4563fbbc7b69c1e3ade))
* **metrics:** prevent double-cumulative histogram bucket counts ([9c5adc9](https://github.com/choiceoh/Deneb/commit/9c5adc9b60361be513dd9aa64a04b741741ab40f))
* **polaris:** correct constants from code review (thresholds, GC, limits) ([04341b7](https://github.com/choiceoh/Deneb/commit/04341b773008167fad124391d59f1754eafa65cb))
* **polaris:** resolve docs directory from repo root, not just workspace ([2a9c873](https://github.com/choiceoh/Deneb/commit/2a9c873f4f99737546334fa176e197a917edbe8f))
* preserve orphaned user messages for re-queue instead of silently dropping them ([#232](https://github.com/choiceoh/Deneb/issues/232)) ([acd506c](https://github.com/choiceoh/Deneb/commit/acd506cabc7f5ebda367eb3f6e86769c23fcf283))
* prevent silent message drops in chat-to-agent delivery pipeline ([#218](https://github.com/choiceoh/Deneb/issues/218)) ([d83245a](https://github.com/choiceoh/Deneb/commit/d83245a6ce3bc621748ca3b57cb2b982e691392b))
* promote toolDeps to server field for cross-phase late-binding ([89382ad](https://github.com/choiceoh/Deneb/commit/89382adddde798373d847589388a48b164dcc1ac))
* **propus:** add standalone workspace to avoid root workspace conflict ([204de2d](https://github.com/choiceoh/Deneb/commit/204de2d837f38487f2e32804a95288e77ba1ecf1))
* **propus:** exclude from root workspace, add standalone workspace members ([242b5a3](https://github.com/choiceoh/Deneb/commit/242b5a3938df6b8499a6c2b5c4078369bf3ff80c))
* **propus:** remove duplicate SaveSession switch case ([0d85b85](https://github.com/choiceoh/Deneb/commit/0d85b85e4328773d9a303749b60d7dfd9ee65df8))
* **propus:** remove invalid app.title from tauri.conf.json for Tauri 2.0 compat ([0223640](https://github.com/choiceoh/Deneb/commit/02236406b84453761133de521422965310a8a93f))
* **propus:** wire StopGeneration abort, fix graceful shutdown, and improve error handling ([168ff47](https://github.com/choiceoh/Deneb/commit/168ff477da7beec8d96686f1453049c913812e4c))
* remove orphaned speech plugin-sdk entrypoints breaking build ([#265](https://github.com/choiceoh/Deneb/issues/265)) ([fa6f507](https://github.com/choiceoh/Deneb/commit/fa6f507d99f1b63f5845be8c7c1a35ac0e1ea91d))
* remove stale daemon-cli build entries left after CLI removal ([#234](https://github.com/choiceoh/Deneb/issues/234)) ([#240](https://github.com/choiceoh/Deneb/issues/240)) ([7ac7b87](https://github.com/choiceoh/Deneb/commit/7ac7b879286fdad1d3a024d190afadf59a4bb98b))
* resolve all failing tests across Rust core and Go gateway ([6dc20d9](https://github.com/choiceoh/Deneb/commit/6dc20d9677438868800493f3b55bca95df12030c))
* resolve autoreply duplicate declarations and model config parsing ([#334](https://github.com/choiceoh/Deneb/issues/334)) ([f7737bd](https://github.com/choiceoh/Deneb/commit/f7737bdc0b157a24db2fc1f588ca4d34f185e8d6))
* resolve build and test bugs across native addon, markdown, and config ([#264](https://github.com/choiceoh/Deneb/issues/264)) ([a7a824c](https://github.com/choiceoh/Deneb/commit/a7a824c1caa141cb4e2b6cde0f1a5b5e2d916dfc))
* resolve Go gateway workspace dir from config instead of os.Getwd() ([#337](https://github.com/choiceoh/Deneb/issues/337)) ([ae6b9a0](https://github.com/choiceoh/Deneb/commit/ae6b9a06a67b401868df151bc3699a7e109d1c9f))
* resolve PR [#184](https://github.com/choiceoh/Deneb/issues/184) merge bugs — missing dep, workspace profiles, CI gaps ([#200](https://github.com/choiceoh/Deneb/issues/200)) ([d0f889e](https://github.com/choiceoh/Deneb/commit/d0f889e1851018cbe28ca730e33d50745223925c))
* resolve recent merge bugs — duplicate Go map keys, stale provider field, wrong bridge call, highway binary path ([#209](https://github.com/choiceoh/Deneb/issues/209)) ([083b698](https://github.com/choiceoh/Deneb/commit/083b698246a357ff291dd663369dd132024b9bc2))
* resolve Rust/Go build errors — missing napi imports, FFI field mismatch, Go test params ([#217](https://github.com/choiceoh/Deneb/issues/217)) ([df5f454](https://github.com/choiceoh/Deneb/commit/df5f454e586989a5d2da3b5a1a581b063e8b92ff))
* separate embedding model from chat model to prevent SGLang 400 errors ([c0c2a01](https://github.com/choiceoh/Deneb/commit/c0c2a0126263556b39639c8bfbd3ee98c727a86d))
* separate embedding model from chat model, auto-launch on DGX Spark ([a626c70](https://github.com/choiceoh/Deneb/commit/a626c70a5f30ebe2d7a7c0a9eeeeac2b14a9bb0d))
* **session:** update keycache_test to match refactored KeyCache struct ([671a723](https://github.com/choiceoh/Deneb/commit/671a72350c194930bbcd532e2c2cdcc0b0de6d98))
* simplify release-please to single package (fix linked-versions crash) ([#261](https://github.com/choiceoh/Deneb/issues/261)) ([11fa93c](https://github.com/choiceoh/Deneb/commit/11fa93cf92e5139e8f38b03c9dbb26761019e37c))
* strip Telegram [@bot](https://github.com/bot) suffix from slash commands ([#344](https://github.com/choiceoh/Deneb/issues/344)) ([2b94c45](https://github.com/choiceoh/Deneb/commit/2b94c45246d37a9adb8079ae3d7766aa972ac11e))
* sync version badges, fix Go bridge race, fix channel-starter types ([#277](https://github.com/choiceoh/Deneb/issues/277)) ([f3d32d9](https://github.com/choiceoh/Deneb/commit/f3d32d9a7022f3c88164088b5f8c075e9dd665cb))
* **telegram:** prevent status reaction context canceled on run completion ([aade952](https://github.com/choiceoh/Deneb/commit/aade9520c4a3a08d81644ae13aa345216833758f))
* **telegram:** reduce dedup TTL from 60s to 10s ([08c1abf](https://github.com/choiceoh/Deneb/commit/08c1abf9d383a6bb468117617585ebc5677ed8db))
* **telegram:** render markdown tables as monospace &lt;pre&gt; blocks ([234929c](https://github.com/choiceoh/Deneb/commit/234929cf5ef972308556ddad0f8475353589a422))
* **telegram:** render markdown tables as monospace &lt;pre&gt; blocks ([8a6452a](https://github.com/choiceoh/Deneb/commit/8a6452a3728822a16167de458909121a24c57b45))
* **telegram:** suppress duplicate reply delivery via recent-send dedup cache ([30e7dee](https://github.com/choiceoh/Deneb/commit/30e7deeb5c67a5160481c01fe494294957827226))
* **vega:** extract JSON array from LLM response with preamble text ([42193d7](https://github.com/choiceoh/Deneb/commit/42193d73837dc670014790a2d87626e37d71a026))
* **vega:** extract JSON array from LLM response with preamble text ([504ce06](https://github.com/choiceoh/Deneb/commit/504ce06f81dc27b6aef124a8420e77cb238af8e1))
* **vega:** extract JSON array from thinking preamble instead of suppressing reasoning ([2a2aa00](https://github.com/choiceoh/Deneb/commit/2a2aa00bec3ed770454db1ac43cdd6f284d5f787))
* **vega:** fix embedding server auto-spawn process exit detection and readiness ([880f59c](https://github.com/choiceoh/Deneb/commit/880f59c102e69bf691104a1e1f5b4a5e2e242135))
* **vega:** force JSON output format in query expansion LLM request ([926bf58](https://github.com/choiceoh/Deneb/commit/926bf5826209ff45ed0d6b3318e360327791a45b))


### ⚡ Performance

* **aurora:** rebalance memory extraction prompts to prioritize personal/relational facts over routine system operations ([2fdf69f](https://github.com/choiceoh/Deneb/commit/2fdf69f2059ee23a02db3fc1b3d4d8778a89e2a0))
* **chat:** speed up multi-tool agent execution ([604020e](https://github.com/choiceoh/Deneb/commit/604020eeefe4dd52680bdd553e1f8ed04df536f5))
* **chat:** speed up multi-tool agent execution ([7aa9ec5](https://github.com/choiceoh/Deneb/commit/7aa9ec59bfd9b2f1ff67b48faf539c73a6748285))
* **chat:** speed up multi-tool agent execution ([5ebc5fa](https://github.com/choiceoh/Deneb/commit/5ebc5fae96a83d10f298162f11176cbd6c5b0058))
* DGX Spark 20-core CPU utilization + chat latency optimization ([#405](https://github.com/choiceoh/Deneb/issues/405)) ([1162f35](https://github.com/choiceoh/Deneb/commit/1162f35f5372dc0ad0ca619afabea1be66b0747b))
* optimize cache eviction, MMR tokenization, compaction cloning, and search caching ([d953ae1](https://github.com/choiceoh/Deneb/commit/d953ae14e333100617eec02153a4863f633577c4))
* **vega:** parallelize semantic search and eliminate sort allocations ([bf54fa5](https://github.com/choiceoh/Deneb/commit/bf54fa5eb1d3dcf53ca6fc8d0132035c207c0f33))


### 🔧 Internal

* activate Go gateway as primary process (Phase 4) ([#274](https://github.com/choiceoh/Deneb/issues/274)) ([e31327b](https://github.com/choiceoh/Deneb/commit/e31327be502bc66458c0071bafec4491c70ee185))
* add .env file loading and upgrade Perplexity to sonar-reasoning-pro ([#412](https://github.com/choiceoh/Deneb/issues/412)) ([664af90](https://github.com/choiceoh/Deneb/commit/664af90ad0ee3d8dcc9463320e4be42ebe3efa3f))
* add link understanding and interactive replies ([#363](https://github.com/choiceoh/Deneb/issues/363)) ([b550469](https://github.com/choiceoh/Deneb/commit/b550469159f8cbd5f555652d61284d77780707b5))
* add missing errors import in ml_cgo.go ([#408](https://github.com/choiceoh/Deneb/issues/408)) ([599d403](https://github.com/choiceoh/Deneb/commit/599d40382f34c6ca87600f2d79229201a0f7f7f1))
* add mtime-based context file caching, TTL memory file list cache, Anthropic prompt cache_control breakpoints, and static Lazy&lt;Regex&gt; for Vega hot paths ([#394](https://github.com/choiceoh/Deneb/issues/394)) ([dbec372](https://github.com/choiceoh/Deneb/commit/dbec37211c941d8a9322fe3482c0e66a0b4e8262))
* add SAFETY comments, fix Mutex unwrap, add module docs ([#380](https://github.com/choiceoh/Deneb/issues/380)) ([71e49fb](https://github.com/choiceoh/Deneb/commit/71e49fb1ad32fd69729197844c567af4bf9f1849))
* **aurora:** centralize dream JSON parsing into callLLMJSON generic ([68c6b0f](https://github.com/choiceoh/Deneb/commit/68c6b0fafebcd6cf04a2c3a99d6efa0d17e240e0))
* auto-start channel plugins via Plugin Host bridge ([#269](https://github.com/choiceoh/Deneb/issues/269)) ([68c7286](https://github.com/choiceoh/Deneb/commit/68c72861f7d041fd0d897aca16784580671a9206))
* **autonomous:** remove auto-goal creation from memory facts ([6bd9e89](https://github.com/choiceoh/Deneb/commit/6bd9e89e975de1f3e40640483d660505780e5d66))
* bypass RPC for model prewarm, call LLM directly ([#362](https://github.com/choiceoh/Deneb/issues/362)) ([7696cfe](https://github.com/choiceoh/Deneb/commit/7696cfefc16d2008c35a4e4afa17c411b5bbcca7))
* **chat:** optimize agent context by deduplicating system prompt and removing redundant tool descriptions ([0bde806](https://github.com/choiceoh/Deneb/commit/0bde8060e32976255e2777ba4bea0450b18726f2))
* **chat:** remove dead relativeTime wrapper, simplify factTemporalAnnotation ([ff21f7c](https://github.com/choiceoh/Deneb/commit/ff21f7c9125bcf0d2e3627ad746792a1e68a3562))
* **chat:** remove unused clipboard and nodes tools, prune 4 inactive skills ([78570c8](https://github.com/choiceoh/Deneb/commit/78570c83b70f058ac9a986e0b70763c2ecee033d))
* **chat:** remove unused clipboard and nodes tools, prune 4 inactive skills ([a68c39d](https://github.com/choiceoh/Deneb/commit/a68c39daea1b9da7786d04b1594a64217ffca8f5))
* enhance /status with server-level system info (uptime, usage, health) ([#392](https://github.com/choiceoh/Deneb/issues/392)) ([b31e28a](https://github.com/choiceoh/Deneb/commit/b31e28abb42a951d08340a9af6787ca0bb8f9b46))
* Extract session and reply types to dedicated types package ([#369](https://github.com/choiceoh/Deneb/issues/369)) ([c39e4e4](https://github.com/choiceoh/Deneb/commit/c39e4e44e85eab1d87393ea66bd8f57d136f8649))
* fix all clippy errors and improve code quality ([#283](https://github.com/choiceoh/Deneb/issues/283)) ([c9c890e](https://github.com/choiceoh/Deneb/commit/c9c890e04fab19985b188f74f03be8288e2c525f))
* fix signal killed by OOM and graceful shutdown ([#361](https://github.com/choiceoh/Deneb/issues/361)) ([6da475a](https://github.com/choiceoh/Deneb/commit/6da475a8ab03fff3e9533d81c1fb075266840cbd))
* fix SystemPromptAddition loss, improve summary quality and context boundaries ([#401](https://github.com/choiceoh/Deneb/issues/401)) ([c625798](https://github.com/choiceoh/Deneb/commit/c625798a06a1a62f12caa5a9b79221db7b0856cc))
* **gateway:** refine startup banner, console logging, and HTTP responses ([68751bb](https://github.com/choiceoh/Deneb/commit/68751bbefc70d06fa5a2aa4b317ccdb2ea59e688))
* Go default runtime + remove 31 dead TS files (3,132 LOC) ([#236](https://github.com/choiceoh/Deneb/issues/236)) ([221b8ad](https://github.com/choiceoh/Deneb/commit/221b8ad0e1f826597b6fae5f5b68feff4841371f))
* graceful SIGTERM before SIGKILL and process group isolation ([#360](https://github.com/choiceoh/Deneb/issues/360)) ([86a66b8](https://github.com/choiceoh/Deneb/commit/86a66b8e908fa3db684c7ddc9f00aa39a19e00e7))
* harden markdown parser and parameterize SQL ID lists ([#388](https://github.com/choiceoh/Deneb/issues/388)) ([2ce5ec2](https://github.com/choiceoh/Deneb/commit/2ce5ec2a2bf8c1e0ce85e23733da726395aa7022))
* increase Go buffers, fix ValidateParams output length, add missing tests ([#268](https://github.com/choiceoh/Deneb/issues/268)) ([4db893d](https://github.com/choiceoh/Deneb/commit/4db893d4fd630cde734838d3fa892dda66e647e5))
* **ml:** remove GGUF/deneb-ml local inference in favor of Gemini + Jina APIs ([f7709f4](https://github.com/choiceoh/Deneb/commit/f7709f4e5d7d01a7f238fb7857794e29e27401cb))
* **ml:** remove GGUF/deneb-ml local inference in favor of Gemini + Jina APIs ([7090da9](https://github.com/choiceoh/Deneb/commit/7090da91d443407d13a6a05e515bae4bd8208f60))
* modernize console handler with decisecond timestamps, pkg tags, separator, and human-friendly durations ([a983420](https://github.com/choiceoh/Deneb/commit/a983420414c9405fd381d557db5aafcd51dd3e34))
* native session execution & agent orchestration (Phase 4) ([#279](https://github.com/choiceoh/Deneb/issues/279)) ([3c28d79](https://github.com/choiceoh/Deneb/commit/3c28d793e1b481d8b52e06b80b6df6f58075a999))
* port context engine to Rust ([#206](https://github.com/choiceoh/Deneb/issues/206)) ([1230e6a](https://github.com/choiceoh/Deneb/commit/1230e6a6baded715bd7e4c063a12ee4f88d50db3))
* **propus:** replace Slint client with Tauri 2.0 + Svelte 5 + Monaco ([c20bb99](https://github.com/choiceoh/Deneb/commit/c20bb994d9f1f7a9d23697a7d59439145e23580f))
* remove OOM score adjustment logic ([#370](https://github.com/choiceoh/Deneb/issues/370)) ([64ae762](https://github.com/choiceoh/Deneb/commit/64ae762098265ad746cadfb5aaefe03fdf0dcfaa))
* replace FFI magic numbers with named constants, strengthen SSRF protection, add AVIF/HEIC/TIFF detection ([#287](https://github.com/choiceoh/Deneb/issues/287)) ([09fbcab](https://github.com/choiceoh/Deneb/commit/09fbcab5b16cff728028c1df3f6b73219909b2c8))
* restrict test-only helpers to #[cfg(test)] and simplify fusion scoring ([0095fef](https://github.com/choiceoh/Deneb/commit/0095fef2e04cb52f9548d08eb81a069893f24709))
* shorten process IDs with base62-encoded nanosecond timestamps ([63a79c9](https://github.com/choiceoh/Deneb/commit/63a79c9811bc0d989a8de935685215c272a7d950))
* skip stale-activity restart when autonomous service is running ([#396](https://github.com/choiceoh/Deneb/issues/396)) ([8c44fd6](https://github.com/choiceoh/Deneb/commit/8c44fd66cbe532ef6657c6d7b71d296f0caca80a))
* split oversized commands_handlers.go and parser.rs into focused domain files ([d1aa584](https://github.com/choiceoh/Deneb/commit/d1aa584899654f2469d584a832c7698d5305b836))
* wire agent cron tool to actual scheduler instead of stubs ([#346](https://github.com/choiceoh/Deneb/issues/346)) ([0c0334c](https://github.com/choiceoh/Deneb/commit/0c0334cfff575780a1578c50f9d34a6e9c79ae87))

## [3.21.1](https://github.com/choiceoh/Deneb/compare/deneb-v3.21.0...deneb-v3.21.1) (2026-03-28)


### 🐛 Bug Fixes

* **propus:** remove duplicate SaveSession switch case ([0d85b85](https://github.com/choiceoh/Deneb/commit/0d85b85e4328773d9a303749b60d7dfd9ee65df8))

## [3.21.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.20.1...deneb-v3.21.0) (2026-03-28)


### ✨ Features

* **propus:** enhance to production level with session save, file delivery, heartbeat, typing, and tests ([475039d](https://github.com/choiceoh/Deneb/commit/475039d1aeaf59742ec123c489901a328079b5ca))
* **propus:** wire tool event broadcasting and coding tool profile ([c437ce8](https://github.com/choiceoh/Deneb/commit/c437ce8d66a50ec8ba41083fae8267b4c5547e36))


### 🐛 Bug Fixes

* **memory:** prevent cascading fact merges in aurora-dream ([0e51696](https://github.com/choiceoh/Deneb/commit/0e51696204a075ea8e99adbfa87f7e2dc4f3c8fa))
* **telegram:** reduce dedup TTL from 60s to 10s ([08c1abf](https://github.com/choiceoh/Deneb/commit/08c1abf9d383a6bb468117617585ebc5677ed8db))
* **telegram:** suppress duplicate reply delivery via recent-send dedup cache ([30e7dee](https://github.com/choiceoh/Deneb/commit/30e7deeb5c67a5160481c01fe494294957827226))

## [3.20.1](https://github.com/choiceoh/Deneb/compare/deneb-v3.20.0...deneb-v3.20.1) (2026-03-28)


### 🐛 Bug Fixes

* **gateway:** access GmailPoll via ConfigSnapshot.Config field ([baab8a0](https://github.com/choiceoh/Deneb/commit/baab8a0d1e5c03bec7e054d05a89c55064f6ae80))
* **memory:** embed Phase 3 pattern facts for future dedup ([840ca6b](https://github.com/choiceoh/Deneb/commit/840ca6b3afd6b5604189f230d379f48167bd99d7))
* **memory:** improve aurora-dream conflict resolution accuracy ([014d069](https://github.com/choiceoh/Deneb/commit/014d0690f284826366da249f6c710d82a831ae0c))
* **memory:** prevent removed facts from being used as conflict winners ([b4e97b1](https://github.com/choiceoh/Deneb/commit/b4e97b1e1ccf49e90e991377f5ac9660dba89198))
* **memory:** retry importance fact extraction on parse failure ([1251025](https://github.com/choiceoh/Deneb/commit/125102518bbb5101cf3493b241af029c910cba5c))
* **memory:** strengthen Phase 2 merge to reduce false conflicts ([1ea54d8](https://github.com/choiceoh/Deneb/commit/1ea54d8cd52721bfde5bc4563fbbc7b69c1e3ade))

## [3.20.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.19.1...deneb-v3.20.0) (2026-03-28)


### ✨ Features

* **cron:** add morning-letter skill and cron job for daily 8AM KST briefing ([4b9b6f5](https://github.com/choiceoh/Deneb/commit/4b9b6f575a549a616ce946a6cf6ee46ce4542ae8))
* **gmail:** add periodic Gmail polling with LLM analysis ([dd64ccb](https://github.com/choiceoh/Deneb/commit/dd64ccbac21b180e72aaa2fce9e763ddd395a458))


### 🐛 Bug Fixes

* **gateway:** remove stale embedResult reference in startup banner ([bec6c84](https://github.com/choiceoh/Deneb/commit/bec6c849bbf456fc8c54273db3b6366db7702ac8))
* **propus:** wire StopGeneration abort, fix graceful shutdown, and improve error handling ([168ff47](https://github.com/choiceoh/Deneb/commit/168ff477da7beec8d96686f1453049c913812e4c))

## [3.19.1](https://github.com/choiceoh/Deneb/compare/deneb-v3.19.0...deneb-v3.19.1) (2026-03-28)


### 🐛 Bug Fixes

* **chat:** add missing RunLogger arg to executeAgentRun call in send_sync ([87f6af5](https://github.com/choiceoh/Deneb/commit/87f6af5ae780420e68a8310d6d4e2c457166f167))

## [3.19.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.18.0...deneb-v3.19.0) (2026-03-28)


### ✨ Features

* **cli:** refine terminal design with Apple aesthetic philosophy ([6f445d8](https://github.com/choiceoh/Deneb/commit/6f445d85381710a5dc506eca31adb104b1a41f07))
* **propus:** add Propus client main.rs ([ba00245](https://github.com/choiceoh/Deneb/commit/ba00245b859697162407c66a343fa8a703bd0008))
* **propus:** add Propus client source and Go channel plugin ([c84c22d](https://github.com/choiceoh/Deneb/commit/c84c22dab66a5ae94d06fac79b8460d5aa2b8938))
* **propus:** add Propus desktop coding channel ([c95c30d](https://github.com/choiceoh/Deneb/commit/c95c30d116047d395d1f37df3552e51e272c3570))
* **propus:** add Propus Slint UI (app.slint) ([5e5f7cc](https://github.com/choiceoh/Deneb/commit/5e5f7cce27e649d9752aa62ef24bb17bd96e1d62))
* **propus:** wire Propus channel registration, chat.send pipeline, and streaming events ([a3bf5d5](https://github.com/choiceoh/Deneb/commit/a3bf5d54113165fd0a52815a9f45f7a95d763399))


### 🐛 Bug Fixes

* **session:** update keycache_test to match refactored KeyCache struct ([671a723](https://github.com/choiceoh/Deneb/commit/671a72350c194930bbcd532e2c2cdcc0b0de6d98))


### ⚡ Performance

* **aurora:** rebalance memory extraction prompts to prioritize personal/relational facts over routine system operations ([2fdf69f](https://github.com/choiceoh/Deneb/commit/2fdf69f2059ee23a02db3fc1b3d4d8778a89e2a0))


### 🔧 Internal

* **autonomous:** remove auto-goal creation from memory facts ([6bd9e89](https://github.com/choiceoh/Deneb/commit/6bd9e89e975de1f3e40640483d660505780e5d66))
* shorten process IDs with base62-encoded nanosecond timestamps ([63a79c9](https://github.com/choiceoh/Deneb/commit/63a79c9811bc0d989a8de935685215c272a7d950))

## [3.18.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.17.0...deneb-v3.18.0) (2026-03-28)


### ✨ Features

* **chat:** add agent detail log system for AI self-diagnostics ([15c25b0](https://github.com/choiceoh/Deneb/commit/15c25b0e1095c62f1b9de7420389ed6443f19b8a))
* **chat:** add agent detail log system for AI self-diagnostics ([7bce087](https://github.com/choiceoh/Deneb/commit/7bce0875d5ab813b6756291580d079b509aaa334))
* **chat:** add git, analyze, test tools and improve existing tools ([f26880d](https://github.com/choiceoh/Deneb/commit/f26880d28646a61a9836574d978aaef631681662))
* **chat:** add git, analyze, test tools and improve existing tools ([e76314c](https://github.com/choiceoh/Deneb/commit/e76314c1e8136bbdb715c398d3845fb4cddc138c))
* **chat:** make agent_logs pilot-only with shortcut and system prompt guidance ([21d434e](https://github.com/choiceoh/Deneb/commit/21d434e07fbba278465ec83186733051ffd326ce))
* **chat:** update default model from glm-5-turbo to glm-5.1 ([a13aee5](https://github.com/choiceoh/Deneb/commit/a13aee5920f3f8f416bfb16df3fd1eb9b4323a5f))
* **chat:** update default model to glm-5.1 ([29d41da](https://github.com/choiceoh/Deneb/commit/29d41da62cfedb1dbccbe693b956e100896c769d))
* **memory:** add data volume trigger for dreaming and fix turn increment bug ([fc8d407](https://github.com/choiceoh/Deneb/commit/fc8d407abb00c264711040ccbfe57931551e2a76))
* **memory:** add data volume trigger for dreaming and fix turn increment bug ([538d5c6](https://github.com/choiceoh/Deneb/commit/538d5c64b5c84d2797870d65f868d99fe745ed65))
* **pilot:** add gateway_logs tool for querying gateway process logs ([9f96cd6](https://github.com/choiceoh/Deneb/commit/9f96cd61bc5e3fb590cff68057b5cbc9a0857c73))
* **pilot:** add gateway_logs tool for querying gateway process logs ([e8ef254](https://github.com/choiceoh/Deneb/commit/e8ef2545fba9c3be531d3144feb830d230c381a1))
* **vega:** add aurora-memory and autonomous health checks to health_check tool ([d9b9cbf](https://github.com/choiceoh/Deneb/commit/d9b9cbfa6ee7c1209071bd8e0317e595c146f1a1))
* **vega:** add health_check tool for embedding, reranker, and sglang diagnostics ([74f4ee2](https://github.com/choiceoh/Deneb/commit/74f4ee23d60b45790341c63e5d1ff8220963951a))
* **vega:** add health_check tool for embedding, reranker, and sglang diagnostics ([fe0ea4f](https://github.com/choiceoh/Deneb/commit/fe0ea4fd36c655b5ef5e01fb51bc8d9303ed8fa7))
* **vega:** add jina-reranker-v2 cross-encoder reranking to search and memory pipelines ([8f39925](https://github.com/choiceoh/Deneb/commit/8f399257aa72ba867ea8d95df7ec3a38daa617e6))


### 🐛 Bug Fixes

* **aurora:** pass Gemini embedder and Jina key as server options to fix init ordering ([5e414e6](https://github.com/choiceoh/Deneb/commit/5e414e69e73218afbfeb51b6583ff0609f3da9c2))
* **aurora:** pass Gemini embedder and Jina key as server options to fix init ordering ([7031b1f](https://github.com/choiceoh/Deneb/commit/7031b1fc6fa69fa29546f2ada9b6fa8da129a375))
* **autonomous:** replace lowest-priority goal instead of rejecting when limit reached ([6952adc](https://github.com/choiceoh/Deneb/commit/6952adc0a0de9e69150aa2fff685f2eedcd43c38))
* **autonomous:** replace lowest-priority goal instead of rejecting when limit reached ([9ec4b50](https://github.com/choiceoh/Deneb/commit/9ec4b501f86ecc693dba08c0a7ecceaeda7f7014))
* **chat:** use cached sglang status instead of active health probe ([74a15d2](https://github.com/choiceoh/Deneb/commit/74a15d2d22a5342a1a9daa91c39a252ab1a1a3b4))
* **memory:** add independent dreaming timer and fix silent failures in ShouldDream ([1c924d7](https://github.com/choiceoh/Deneb/commit/1c924d783a4580bcf6a834feb239f5a62f15e81a))
* **memory:** increase importance maxTokens to 1536 and add truncated JSON recovery ([a0ddcdf](https://github.com/choiceoh/Deneb/commit/a0ddcdf70e343ab4c0eab352b5cc0f95b31d3684))
* **memory:** increase importance maxTokens to 1536 and add truncated JSON recovery ([0663f59](https://github.com/choiceoh/Deneb/commit/0663f59ae073b67e3f8ed3032c9d7a538b44bf9a))
* **telegram:** render markdown tables as monospace &lt;pre&gt; blocks ([234929c](https://github.com/choiceoh/Deneb/commit/234929cf5ef972308556ddad0f8475353589a422))
* **telegram:** render markdown tables as monospace &lt;pre&gt; blocks ([8a6452a](https://github.com/choiceoh/Deneb/commit/8a6452a3728822a16167de458909121a24c57b45))
* **vega:** extract JSON array from LLM response with preamble text ([42193d7](https://github.com/choiceoh/Deneb/commit/42193d73837dc670014790a2d87626e37d71a026))
* **vega:** extract JSON array from LLM response with preamble text ([504ce06](https://github.com/choiceoh/Deneb/commit/504ce06f81dc27b6aef124a8420e77cb238af8e1))
* **vega:** extract JSON array from thinking preamble instead of suppressing reasoning ([2a2aa00](https://github.com/choiceoh/Deneb/commit/2a2aa00bec3ed770454db1ac43cdd6f284d5f787))
* **vega:** force JSON output format in query expansion LLM request ([926bf58](https://github.com/choiceoh/Deneb/commit/926bf5826209ff45ed0d6b3318e360327791a45b))


### ⚡ Performance

* **chat:** speed up multi-tool agent execution ([604020e](https://github.com/choiceoh/Deneb/commit/604020eeefe4dd52680bdd553e1f8ed04df536f5))
* **chat:** speed up multi-tool agent execution ([7aa9ec5](https://github.com/choiceoh/Deneb/commit/7aa9ec59bfd9b2f1ff67b48faf539c73a6748285))
* **chat:** speed up multi-tool agent execution ([5ebc5fa](https://github.com/choiceoh/Deneb/commit/5ebc5fae96a83d10f298162f11176cbd6c5b0058))


### 🔧 Internal

* **chat:** remove unused clipboard and nodes tools, prune 4 inactive skills ([78570c8](https://github.com/choiceoh/Deneb/commit/78570c83b70f058ac9a986e0b70763c2ecee033d))
* **chat:** remove unused clipboard and nodes tools, prune 4 inactive skills ([a68c39d](https://github.com/choiceoh/Deneb/commit/a68c39daea1b9da7786d04b1594a64217ffca8f5))
* **ml:** remove GGUF/deneb-ml local inference in favor of Gemini + Jina APIs ([f7709f4](https://github.com/choiceoh/Deneb/commit/f7709f4e5d7d01a7f238fb7857794e29e27401cb))
* **ml:** remove GGUF/deneb-ml local inference in favor of Gemini + Jina APIs ([7090da9](https://github.com/choiceoh/Deneb/commit/7090da91d443407d13a6a05e515bae4bd8208f60))

## [3.17.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.16.0...deneb-v3.17.0) (2026-03-27)


### ✨ Features

* **autonomous:** auto-set goals from recalled memory facts during knowledge prefetch ([97a285a](https://github.com/choiceoh/Deneb/commit/97a285a8f5a166bdadc74d537942ad6c16a62bdd))
* **chat:** add multi_edit, tree, diff coding tools ([4e1f9dd](https://github.com/choiceoh/Deneb/commit/4e1f9dde35896982b8978100cec40c384ec2eb4e))
* **memory:** add category-aware importance, recency, and frequency weighting ([4437e71](https://github.com/choiceoh/Deneb/commit/4437e71c0d84f87bc94f36240e282845f28bde34))
* **pilot:** add shortcuts for gmail, youtube, polaris, image, clipboard, ls, vega and register vega chat tool ([7f17030](https://github.com/choiceoh/Deneb/commit/7f170300d3cb7e9017158bb31221095d770eefc7))
* **polaris:** add 5 new guides and update existing guide content ([ee55306](https://github.com/choiceoh/Deneb/commit/ee553069f263677722768d4c84798fe614da8a8d))


### 🐛 Bug Fixes

* **memory:** enforce JSON mode and strip thinking tags in AuroraDream dreaming ([86527f5](https://github.com/choiceoh/Deneb/commit/86527f5a64d800d7fbf0c0399a5125125404c088))
* **polaris:** resolve docs directory from repo root, not just workspace ([2a9c873](https://github.com/choiceoh/Deneb/commit/2a9c873f4f99737546334fa176e197a917edbe8f))

## [3.16.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.15.0...deneb-v3.16.0) (2026-03-27)


### ✨ Features

* **chat:** add temporal context awareness to memory fact display ([bfe14a5](https://github.com/choiceoh/Deneb/commit/bfe14a50bd04c5099325336cdc5e5c5b761ea478))
* **polaris:** add FFI bridge, RPC, auth details to architecture guide ([19d5dcf](https://github.com/choiceoh/Deneb/commit/19d5dcf18c1ec65c47df74e7e8c9075c431746fc))
* **polaris:** enrich 15 existing guides and add 8 new tool guides ([3124ed7](https://github.com/choiceoh/Deneb/commit/3124ed7af32d293160c0e06822a332109054cebe))


### 🐛 Bug Fixes

* **chat:** improve temporal annotations with natural Korean and two-tier volatility ([3ca53ee](https://github.com/choiceoh/Deneb/commit/3ca53ee90b2651e97d9f21b62413bf4553aae4a0))
* **core:** replace mutex unwrap with poison-recovery in NAPI FFI boundary ([c250eaf](https://github.com/choiceoh/Deneb/commit/c250eaf1b57f757912c7f1d848d6fa3fb17756b7))
* **memory:** align importance JSON parsing with json_object response format ([abb8e32](https://github.com/choiceoh/Deneb/commit/abb8e3285d479a0458355a3e5c50b563ef12106f))
* **polaris:** correct constants from code review (thresholds, GC, limits) ([04341b7](https://github.com/choiceoh/Deneb/commit/04341b773008167fad124391d59f1754eafa65cb))


### 🔧 Internal

* **chat:** remove dead relativeTime wrapper, simplify factTemporalAnnotation ([ff21f7c](https://github.com/choiceoh/Deneb/commit/ff21f7c9125bcf0d2e3627ad746792a1e68a3562))

## [3.15.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.14.0...deneb-v3.15.0) (2026-03-27)


### ✨ Features

* **aurora:** add Aurora desktop RPC channel handlers ([d2efd4e](https://github.com/choiceoh/Deneb/commit/d2efd4e12308fe1cbb6c3a12016d1f68470f98eb))
* **aurora:** Aurora 데스크톱 RPC 채널 핸들러 ([467cff3](https://github.com/choiceoh/Deneb/commit/467cff3ce07811dd5838569152550957c47ee87a))
* **polaris:** improve manual with compact topics, better search, new guides ([31ecdc5](https://github.com/choiceoh/Deneb/commit/31ecdc58097e96b7da438f5d47e42dc7a149922f))


### 🐛 Bug Fixes

* **gateway:** rollback Go version from 1.25.0 to 1.24.7 ([0091bd9](https://github.com/choiceoh/Deneb/commit/0091bd90f3aed53a9f08be0bd6aaf67d4572fc64))
* **memory:** prevent JSON parse failures in importance extraction ([3fb0f5b](https://github.com/choiceoh/Deneb/commit/3fb0f5b0c8f05f185827ae361c8e8a35800c8b5f))
* **metrics:** prevent double-cumulative histogram bucket counts ([9c5adc9](https://github.com/choiceoh/Deneb/commit/9c5adc9b60361be513dd9aa64a04b741741ab40f))
* **vega:** fix embedding server auto-spawn process exit detection and readiness ([880f59c](https://github.com/choiceoh/Deneb/commit/880f59c102e69bf691104a1e1f5b4a5e2e242135))


### ⚡ Performance

* optimize cache eviction, MMR tokenization, compaction cloning, and search caching ([d953ae1](https://github.com/choiceoh/Deneb/commit/d953ae14e333100617eec02153a4863f633577c4))
* **vega:** parallelize semantic search and eliminate sort allocations ([bf54fa5](https://github.com/choiceoh/Deneb/commit/bf54fa5eb1d3dcf53ca6fc8d0132035c207c0f33))


### 🔧 Internal

* **chat:** optimize agent context by deduplicating system prompt and removing redundant tool descriptions ([0bde806](https://github.com/choiceoh/Deneb/commit/0bde8060e32976255e2777ba4bea0450b18726f2))
* restrict test-only helpers to #[cfg(test)] and simplify fusion scoring ([0095fef](https://github.com/choiceoh/Deneb/commit/0095fef2e04cb52f9548d08eb81a069893f24709))

## [3.14.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.13.0...deneb-v3.14.0) (2026-03-27)


### ✨ Features

* add autonomous tool to agent tool registry ([903755f](https://github.com/choiceoh/Deneb/commit/903755f794dc196ff218b2e64e286188d16fabea))
* add autonomous tool to system prompt descriptions and tool order ([88871ec](https://github.com/choiceoh/Deneb/commit/88871ec29b2e7e24e4a7cd30379c73c934f53000))
* add mutual understanding tracking to Aurora Dream ([b6c7de9](https://github.com/choiceoh/Deneb/commit/b6c7de9faa965cf960061d082d319884b71af411))
* add system_manual tool for queryable Deneb documentation ([60eff64](https://github.com/choiceoh/Deneb/commit/60eff64d798cab3a4adc0e279eafb01cd239f6b1))
* compaction + quality filtering for MEMORY.md ([ab06a40](https://github.com/choiceoh/Deneb/commit/ab06a40372e0b833b8d96b71be339d63cee00316))
* deepen mutual understanding tracking ([347529a](https://github.com/choiceoh/Deneb/commit/347529ae0303fb6d0ac2ef04a535c82058c42c25))
* deepen mutual understanding with real-time signals, history, and cross-phase integration ([e97e0c1](https://github.com/choiceoh/Deneb/commit/e97e0c1e271375492cac868601d1390a55a98488))
* fix review issues — 2 bugs, 4 logic, 2 prompt, 1 style ([dab41be](https://github.com/choiceoh/Deneb/commit/dab41be151ea2ba7980576047ad72113146a9f31))
* fix second review — UTF-8 safety, sql.ErrNoRows, signal cleanup, tests ([f4755cc](https://github.com/choiceoh/Deneb/commit/f4755cc468052d72413cd03f12379cfac0affb00))
* fix updateUserModelFromFact reading from wrong table ([9a51dae](https://github.com/choiceoh/Deneb/commit/9a51dae03598fd690e6ce87ce3435270048ac767))
* rename system_manual tool to polaris ([815d438](https://github.com/choiceoh/Deneb/commit/815d4383b447d2c4df1aadf90125b91500e997d1))


### 🐛 Bug Fixes

* add module prefixes to release-please changelog-sections ([e7471e7](https://github.com/choiceoh/Deneb/commit/e7471e753aa012c8fc784651e14d6ed9f4a4d852))
* promote toolDeps to server field for cross-phase late-binding ([89382ad](https://github.com/choiceoh/Deneb/commit/89382adddde798373d847589388a48b164dcc1ac))
* **telegram:** prevent status reaction context canceled on run completion ([aade952](https://github.com/choiceoh/Deneb/commit/aade9520c4a3a08d81644ae13aa345216833758f))


### 🔧 Internal

* **gateway:** refine startup banner, console logging, and HTTP responses ([68751bb](https://github.com/choiceoh/Deneb/commit/68751bbefc70d06fa5a2aa4b317ccdb2ea59e688))

## [3.13.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.12.0...deneb-v3.13.0) (2026-03-27)

### ✨ Features

* add LiteParse integration for document parsing ([9b91fda](https://github.com/choiceoh/Deneb/commit/9b91fda))
* replace simple typing ticker with phase-aware FullTypingSignaler ([def5a92](https://github.com/choiceoh/Deneb/commit/def5a92))
* add status reaction emoji on user message during agent runs ([3351488](https://github.com/choiceoh/Deneb/commit/3351488))
* add cargo-deny config, DuckDB analytics scripts, and Makefile targets ([9793986c25b600ad38ebcf1a2b22ca9cafb26b5d](https://github.com/choiceoh/Deneb/commit/9793986c25b600ad38ebcf1a2b22ca9cafb26b5d))
* **go:** add property-based and benchmark tests for session and RPC ([bee0d077e6f7b8d0f89e865c010f8cc90308480d](https://github.com/choiceoh/Deneb/commit/bee0d077e6f7b8d0f89e865c010f8cc90308480d))
* **go:** add stdlib metrics package with Prometheus-compatible /metrics endpoint ([dd9861d4de54628e8da1763a275219a0e31dd280](https://github.com/choiceoh/Deneb/commit/dd9861d4de54628e8da1763a275219a0e31dd280))
* **rust:** add proptest property tests for protocol frames and cosine similarity ([22612028307ed571158339519fd764e4f509c316](https://github.com/choiceoh/Deneb/commit/22612028307ed571158339519fd764e4f509c316))

### 🐛 Bug Fixes

* fix status reactions with Telegram-compatible emojis and error logging ([41a5099](https://github.com/choiceoh/Deneb/commit/41a5099))

## [3.12.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.7...deneb-v3.12.0) (2026-03-27)

### ✨ Features

* auto-detect embedding server on DGX Spark startup ([affd107512bea1ad75993eff3bf0558045992ba0](https://github.com/choiceoh/Deneb/commit/affd107512bea1ad75993eff3bf0558045992ba0))
* auto-launch SGLang embedding server on DGX Spark ([08620fe4641780790eb2d4a9120a50a48439941a](https://github.com/choiceoh/Deneb/commit/08620fe4641780790eb2d4a9120a50a48439941a))

### 🐛 Bug Fixes

* separate embedding model from chat model to prevent SGLang 400 errors ([c0c2a0126263556b39639c8bfbd3ee98c727a86d](https://github.com/choiceoh/Deneb/commit/c0c2a0126263556b39639c8bfbd3ee98c727a86d))
* separate embedding model from chat model, auto-launch on DGX Spark ([a626c70a5f30ebe2d7a7c0a9eeeeac2b14a9bb0d](https://github.com/choiceoh/Deneb/commit/a626c70a5f30ebe2d7a7c0a9eeeeac2b14a9bb0d))

## [3.11.7](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.6...deneb-v3.11.7) (2026-03-26)

### ✨ Features

* enhance inter-tool integration ([08c061f](https://github.com/choiceoh/Deneb/commit/08c061f))
* enrich tool descriptions in system prompt ([47f70c7](https://github.com/choiceoh/Deneb/commit/47f70c7))
* replace GGUF models with SGLang for embedding and query expansion ([a15721e](https://github.com/choiceoh/Deneb/commit/a15721e))
* add gmail tool with inbox summary, search, send, reply, labels and contact aliases ([4cf9158](https://github.com/choiceoh/Deneb/commit/4cf9158))
* add gmail tool usage guide to system prompt tool selection ([39c24d2](https://github.com/choiceoh/Deneb/commit/39c24d2))
* add Honcho-inspired structured memory system with SGLang ([8b37aa2](https://github.com/choiceoh/Deneb/commit/8b37aa2))
* deep improvements — gateway init, dedup, migration, conflict resolution, Korean FTS, mid-run extraction, Neuromancer-style prompts ([fb317cd](https://github.com/choiceoh/Deneb/commit/fb317cd))
* wire StartPeriodicTimer at gateway init ([0b9aad0](https://github.com/choiceoh/Deneb/commit/0b9aad0))

### ⚡ Performance

* optimize inbound pipeline latency (async handler, reduced timeouts) ([f0c6d3c](https://github.com/choiceoh/Deneb/commit/f0c6d3c))
* improve cache utilization across hot paths ([54e1d4a](https://github.com/choiceoh/Deneb/commit/54e1d4a))
* parallelize knowledge+context prep, add pipeline timing, reduce proactive timeout ([7cfec95](https://github.com/choiceoh/Deneb/commit/7cfec95))
* reduce compress timeout (30s→10s) and raise threshold (8K→16K) ([e27790d](https://github.com/choiceoh/Deneb/commit/e27790d))

### 🐛 Bug Fixes

* resolve all failing tests across Rust core and Go gateway ([6dc20d9677438868800493f3b55bca95df12030c](https://github.com/choiceoh/Deneb/commit/6dc20d9677438868800493f3b55bca95df12030c))
* fix code review issues ([9d7804d](https://github.com/choiceoh/Deneb/commit/9d7804d))
* fix deadlock, race condition, and deduplicate stream helpers ([276146c](https://github.com/choiceoh/Deneb/commit/276146c))

## [3.11.6](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.5...deneb-v3.11.6) (2026-03-26)

### ✨ Features

* include workspace directory in sglang system prompt ([046cd6d](https://github.com/choiceoh/Deneb/commit/046cd6d))
* add .env file loading and upgrade Perplexity to sonar-reasoning-pro ([664af90](https://github.com/choiceoh/Deneb/commit/664af90))
* Add sessions_search and sessions_restore tools for transcript management ([545b45e](https://github.com/choiceoh/Deneb/commit/545b45e))
* add health check, thinking mode, chaining, smart truncation, and metrics ([75061a7](https://github.com/choiceoh/Deneb/commit/75061a7))
* add JSON response cleaning for output_format json ([6bd62be](https://github.com/choiceoh/Deneb/commit/6bd62be))
* integrate Vega + Memory knowledge prefetch into context assembly ([b531601](https://github.com/choiceoh/Deneb/commit/b531601))
* improve agent tool usage, response speed, and action efficiency ([e82df17](https://github.com/choiceoh/Deneb/commit/e82df17))
* add output post-processing — markdown normalization, list cleaning, length enforcement ([5d6b14c](https://github.com/choiceoh/Deneb/commit/5d6b14c))

### 🐛 Bug Fixes

* add missing errors import in ml_cgo.go ([599d403](https://github.com/choiceoh/Deneb/commit/599d403))
* fix truncation overlap panic, brief+thinking conflict, chain self-call guard ([35f7f8e](https://github.com/choiceoh/Deneb/commit/35f7f8e))
* fix review issues — UTF-8 truncation, Anthropic path, short message filter ([d0014f2](https://github.com/choiceoh/Deneb/commit/d0014f2))
* use MarkdownToTelegramHTML for reply formatting (fixes fenced code blocks) ([5db3d35](https://github.com/choiceoh/Deneb/commit/5db3d35))

### 🔧 Internal

* split oversized commands_handlers.go and parser.rs into focused domain files ([d1aa584899654f2469d584a832c7698d5305b836](https://github.com/choiceoh/Deneb/commit/d1aa584899654f2469d584a832c7698d5305b836))
* modernize console handler with decisecond timestamps, pkg tags, separator, and human-friendly durations ([a983420](https://github.com/choiceoh/Deneb/commit/a983420))

## [3.11.5](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.4...deneb-v3.11.5) (2026-03-26)

### ✨ Features

* graceful SIGTERM before SIGKILL and process group isolation ([86a66b8](https://github.com/choiceoh/Deneb/commit/86a66b8))
* add link understanding and interactive replies ([b550469](https://github.com/choiceoh/Deneb/commit/b550469))
* implement subagents tool with session manager integration ([ea085d2](https://github.com/choiceoh/Deneb/commit/ea085d2))
* implement full ACP RPC subsystem ([dda6764](https://github.com/choiceoh/Deneb/commit/dda6764))
* Port channel utilities from TypeScript to Go ([68b9806](https://github.com/choiceoh/Deneb/commit/68b9806))
* Add link understanding and reply context for Telegram messages ([5bfe66a](https://github.com/choiceoh/Deneb/commit/5bfe66a))
* Add media extraction: YouTube transcripts, video frames, and Telegram attachments ([efd5dbd](https://github.com/choiceoh/Deneb/commit/efd5dbd))
* Add human-readable console log handler with optional color output ([b672520](https://github.com/choiceoh/Deneb/commit/b672520))
* Add autonomous goal-driven execution system with attention-based triggering ([4db1867](https://github.com/choiceoh/Deneb/commit/4db1867))
* Add goal starvation detection, note history, and stale goal auto-pause ([ab82843](https://github.com/choiceoh/Deneb/commit/ab82843))
* enhance /status with server-level system info ([b31e28a](https://github.com/choiceoh/Deneb/commit/b31e28a))
* deepen TS→Go port: ChannelsConfig types, cron run log wiring, wizard multi-step ([73a0ebd](https://github.com/choiceoh/Deneb/commit/73a0ebd))
* Increase context limits and adjust chat configuration defaults ([2a519db](https://github.com/choiceoh/Deneb/commit/2a519db))
* add sglang fallback and local summarization ([9b48bf8](https://github.com/choiceoh/Deneb/commit/9b48bf8))
* add send_file, http, kv, clipboard agent tools ([37c6645](https://github.com/choiceoh/Deneb/commit/37c6645))
* optimize AI agent tools for parallel execution and richer schemas ([b616bd6](https://github.com/choiceoh/Deneb/commit/b616bd6))
* enhance tool schemas with enum/default constraints ([99e9150](https://github.com/choiceoh/Deneb/commit/99e9150))
* Unified web tool: search, fetch, and search+fetch in one ([6a34382](https://github.com/choiceoh/Deneb/commit/6a34382))
* Add Pilot tool and Copilot background monitor with local sglang ([78a33fb](https://github.com/choiceoh/Deneb/commit/78a33fb))

### ⚡ Performance

* DGX Spark 20-core CPU utilization + chat latency optimization ([405](https://github.com/choiceoh/Deneb/issues/405)) ([1162f35f5372dc0ad0ca619afabea1be66b0747b](https://github.com/choiceoh/Deneb/commit/1162f35f5372dc0ad0ca619afabea1be66b0747b))
* bypass RPC for model prewarm, call LLM directly ([7696cfe](https://github.com/choiceoh/Deneb/commit/7696cfe))
* add mtime-based context file caching, TTL memory file list cache, Anthropic prompt cache_control breakpoints ([dbec372](https://github.com/choiceoh/Deneb/commit/dbec372))
* Optimize for DGX Spark: SIMD, parallelism, and caching ([957aa1b](https://github.com/choiceoh/Deneb/commit/957aa1b))

### 🐛 Bug Fixes

* fix signal killed by OOM and graceful shutdown ([6da475a](https://github.com/choiceoh/Deneb/commit/6da475a))
* remove OOM score adjustment logic ([64ae762](https://github.com/choiceoh/Deneb/commit/64ae762))
* fix clippy errors, Go formatting, proto generation ([8e75340](https://github.com/choiceoh/Deneb/commit/8e75340))
* autonomous: fix bugs, deadlock risks, and add comprehensive tests ([1a66bda](https://github.com/choiceoh/Deneb/commit/1a66bda))
* core-rs: harden markdown parser and parameterize SQL ID lists ([2ce5ec2](https://github.com/choiceoh/Deneb/commit/2ce5ec2))
* autonomous: fix production issues — transcript reset, error tracking, enabled persistence ([dba1f21](https://github.com/choiceoh/Deneb/commit/dba1f21))
* watchdog: skip stale-activity restart when autonomous service is running ([8c44fd6](https://github.com/choiceoh/Deneb/commit/8c44fd6))
* compaction: fix SystemPromptAddition loss, improve summary quality ([c625798](https://github.com/choiceoh/Deneb/commit/c625798))

### 🔧 Internal

* Improve code clarity: add mutex guards and simplify error handling ([b8d0ce6](https://github.com/choiceoh/Deneb/commit/b8d0ce6))
* core-rs: add SAFETY comments, fix Mutex unwrap, add module docs ([71e49fb](https://github.com/choiceoh/Deneb/commit/71e49fb))
* cleanup: remove unused folders and files ([722e881](https://github.com/choiceoh/Deneb/commit/722e881))
* Refactor RPC handlers into domain-based subpackages ([75063a7](https://github.com/choiceoh/Deneb/commit/75063a7))
* Extract session and reply types to dedicated types package ([c39e4e4](https://github.com/choiceoh/Deneb/commit/c39e4e4))

## [3.11.4](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.3...deneb-v3.11.4) (2026-03-26)

### ✨ Features

* add SOUL.md activation instruction to system prompt ([5284377](https://github.com/choiceoh/Deneb/commit/5284377))
* implement Go host-side orchestration for Rust compaction engine ([8406e00](https://github.com/choiceoh/Deneb/commit/8406e00))
* implement stub tools and ACP wiring ([6d375cd](https://github.com/choiceoh/Deneb/commit/6d375cd))

### 🐛 Bug Fixes

* correct FFI error codes for session key validation and buffer-too-small returns ([352](https://github.com/choiceoh/Deneb/issues/352)) ([8f882ccaffee5b0a8e722bf7146b3f3d326750bc](https://github.com/choiceoh/Deneb/commit/8f882ccaffee5b0a8e722bf7146b3f3d326750bc))
* downgrade context canceled polling error to info level ([90bba33](https://github.com/choiceoh/Deneb/commit/90bba33))
* fix WebSocket connectivity, session GC, and channel restart reliability ([6eab080](https://github.com/choiceoh/Deneb/commit/6eab080))

## [3.11.3](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.2...deneb-v3.11.3) (2026-03-26)

### ✨ Features

* wire agent cron tool to actual scheduler instead of stubs ([0c0334c](https://github.com/choiceoh/Deneb/commit/0c0334c))
* wire disconnected packages to RPC/server layer ([43ee8fb](https://github.com/choiceoh/Deneb/commit/43ee8fb))

### 🐛 Bug Fixes

* strip Telegram @bot suffix from slash commands ([344](https://github.com/choiceoh/Deneb/issues/344)) ([2b94c45246d37a9adb8079ae3d7766aa972ac11e](https://github.com/choiceoh/Deneb/commit/2b94c45246d37a9adb8079ae3d7766aa972ac11e))

### 🔧 Internal

* remove AGENTS.md symlink, make CLAUDE.md the real file ([44fb8a9](https://github.com/choiceoh/Deneb/commit/44fb8a9))
* remove JS/TS/Python dependencies and all references ([4da9110](https://github.com/choiceoh/Deneb/commit/4da9110))

## [3.11.2](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.1...deneb-v3.11.2) (2026-03-26)

### 🐛 Bug Fixes

* handle agents.defaults.model as json.RawMessage (string or object) ([339](https://github.com/choiceoh/Deneb/issues/339)) ([ad11af9fcd58abe4f175034020034cc0b90d3a91](https://github.com/choiceoh/Deneb/commit/ad11af9fcd58abe4f175034020034cc0b90d3a91))
* agents.defaults.model parsing + memory_search diagnostics ([c08f09e](https://github.com/choiceoh/Deneb/commit/c08f09e))

### 🔧 Internal

* Go/Rust 마이그레이션 평가 및 잔여 격차 해소 ([8a44a86](https://github.com/choiceoh/Deneb/commit/8a44a86))

## [3.11.1](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.0...deneb-v3.11.1) (2026-03-26)

### 🐛 Bug Fixes

* resolve autoreply duplicate declarations and model config parsing ([334](https://github.com/choiceoh/Deneb/issues/334)) ([f7737bdc0b157a24db2fc1f588ca4d34f185e8d6](https://github.com/choiceoh/Deneb/commit/f7737bdc0b157a24db2fc1f588ca4d34f185e8d6))
* resolve Go gateway workspace dir from config instead of os.Getwd() ([337](https://github.com/choiceoh/Deneb/issues/337)) ([ae6b9a06a67b401868df151bc3699a7e109d1c9f](https://github.com/choiceoh/Deneb/commit/ae6b9a06a67b401868df151bc3699a7e109d1c9f))

### 🔧 Internal

* Remove TypeScript codebase entirely ([50aba9c](https://github.com/choiceoh/Deneb/commit/50aba9c))

## [3.11.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.10.0...deneb-v3.11.0) (2026-03-26)

### ✨ Features

* complete Python-to-Rust migration for Vega ([#304](https://github.com/choiceoh/Deneb/issues/304)) ([e93e541](https://github.com/choiceoh/Deneb/commit/e93e541))
* skills: enhance GitHub skill with diff, review, search, labels, releases, and best practices ([955a8ac](https://github.com/choiceoh/Deneb/commit/955a8ac))
* telegram: use detached context for polling goroutine ([ca80cac](https://github.com/choiceoh/Deneb/commit/ca80cac))
* telegram: fix HTTP client timeout shorter than long-poll timeout ([70aed49](https://github.com/choiceoh/Deneb/commit/70aed49))
* telegram: store handler on Plugin so it survives until bot Start ([3a8fa10](https://github.com/choiceoh/Deneb/commit/3a8fa10))
* telegram: add edited_message, edited_channel_post, my_chat_member handlers and narrow allowed-updates ([4950861](https://github.com/choiceoh/Deneb/commit/4950861))
* add tool calling and vision support to OpenAI streaming ([9619444](https://github.com/choiceoh/Deneb/commit/9619444))
* enhance media parsing, MIME detection, and security filtering ([e36509d](https://github.com/choiceoh/Deneb/commit/e36509d))
* agent-runtime: scaffold Rust crate for agent subsystem port ([ebfab22](https://github.com/choiceoh/Deneb/commit/ebfab22))
* add HTTP webhook handlers for hooks, OpenAI chat, and Responses APIs ([98e5b78](https://github.com/choiceoh/Deneb/commit/98e5b78))
* vega: port missing Python features to Rust (E-2 through E-7) ([b6a6ee9](https://github.com/choiceoh/Deneb/commit/b6a6ee9))
* add HTTP API endpoints and auth security hardening ([a8f633a](https://github.com/choiceoh/Deneb/commit/a8f633a))
* implement core agent tools and system prompt generation ([7394e6c](https://github.com/choiceoh/Deneb/commit/7394e6c))
* port cron store migration, delivery, and validation logic to Go ([25331a6](https://github.com/choiceoh/Deneb/commit/25331a6))
* port subagent commands and utility infrastructure from TypeScript ([6163555](https://github.com/choiceoh/Deneb/commit/6163555))
* port plugin discovery, provider runtime, and validation to Go ([54e15c1](https://github.com/choiceoh/Deneb/commit/54e15c1))
* port subagent commands and followup queue system to Go ([e049ec9](https://github.com/choiceoh/Deneb/commit/e049ec9))
* port auto-reply core logic from TypeScript to Go ([363801f](https://github.com/choiceoh/Deneb/commit/363801f))
* port autoreply directive parsing and pipeline logic from TypeScript to Go ([05b75d7](https://github.com/choiceoh/Deneb/commit/05b75d7))

### 🐛 Bug Fixes

* correct Rust base64 test assertion, Go ML test stub handling, and format drift ([#316](https://github.com/choiceoh/Deneb/issues/316)) ([19712ee](https://github.com/choiceoh/Deneb/commit/19712ee))
* **gateway-go:** fix Telegram chat handler bugs — unique request IDs, reply timeouts, strict channel filter ([#311](https://github.com/choiceoh/Deneb/issues/311)) ([3a96b01](https://github.com/choiceoh/Deneb/commit/3a96b01))
* harden Go/Rust FFI build — buffer growth, handle safety, error codes ([#298](https://github.com/choiceoh/Deneb/issues/298)) ([93c68a6](https://github.com/choiceoh/Deneb/commit/93c68a6))
* autoreply: fix duplicate type declarations causing build failure ([ef1c183](https://github.com/choiceoh/Deneb/commit/ef1c183))

### 🔧 Internal

* fix chat test compilation after bridge removal ([d3ad4d7](https://github.com/choiceoh/Deneb/commit/d3ad4d7))
* add SIGUSR1 graceful restart support ([677165b](https://github.com/choiceoh/Deneb/commit/677165b))
* replace TypeScript Compiler API with oxc-parser, switch tsc to tsgo ([bf85c21](https://github.com/choiceoh/Deneb/commit/bf85c21))
* auto-start registered channels on gateway boot ([bc1c962](https://github.com/choiceoh/Deneb/commit/bc1c962))
* enhance RPC methods — cleanup, new methods, improvements ([20cb4f8](https://github.com/choiceoh/Deneb/commit/20cb4f8))
* wire Telegram messages to chat handler for end-to-end replies ([c6d6d3f](https://github.com/choiceoh/Deneb/commit/c6d6d3f))
* replace Python legacy with Go and shell native alternatives ([64fcf93](https://github.com/choiceoh/Deneb/commit/64fcf93))
* add OpenAI-compatible LLM client and config-based provider resolution ([f7798fa](https://github.com/choiceoh/Deneb/commit/f7798fa))
* fix silent error handling, add message size validation, and event drop logging ([fc07810](https://github.com/choiceoh/Deneb/commit/fc07810))
* identify and fix missing connections across codebase ([1302f6f](https://github.com/choiceoh/Deneb/commit/1302f6f))
* port Phase 4 TS business logic to Go ([8a379f3](https://github.com/choiceoh/Deneb/commit/8a379f3))
* unify HybridVectorResult/HybridKeywordResult into single HybridResult ([4de1a39](https://github.com/choiceoh/Deneb/commit/4de1a39))
## [3.10.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.9.0...deneb-v3.10.0) (2026-03-25)

### Features

- port 11 OpenClaw 3.22/3.23 features (security, performance, Telegram) ([#294](https://github.com/choiceoh/Deneb/issues/294)) ([9ab2fe6](https://github.com/choiceoh/Deneb/commit/9ab2fe6976238aab43bccd0035786a2de4fbfa15))

## [3.9.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.8.0...deneb-v3.9.0) (2026-03-25)

### Features

- add Highway — Rust-native high-performance test orchestration engine ([#159](https://github.com/choiceoh/Deneb/issues/159)) ([8fc32a1](https://github.com/choiceoh/Deneb/commit/8fc32a14e99415baa4e1ef6898420c3b5771fd67))
- add Rust core library (core-rs) with napi-rs bindings ([#168](https://github.com/choiceoh/Deneb/issues/168)) ([49056eb](https://github.com/choiceoh/Deneb/commit/49056ebcc139c53b7e5b241d2fe65742267cfca2))
- centralize version management and release notes ([#54](https://github.com/choiceoh/Deneb/issues/54)) ([a1f94b3](https://github.com/choiceoh/Deneb/commit/a1f94b39911753bf0e0769214bf178db19f5e2ec))
- compaction notification via Telegram on compact ([6b19a08](https://github.com/choiceoh/Deneb/commit/6b19a084211680dd814747915b9aa3070b5e0776))
- dynamic channel registry (PoC) ([50b8a84](https://github.com/choiceoh/Deneb/commit/50b8a8485a724e5c6a78ad88e43488f3a292d4c0))
- replace lobster 🦞 with blue star ⭐ branding ([5c55012](https://github.com/choiceoh/Deneb/commit/5c550129001fd13e463d2e8cf727f66c0221a593))
- **vega:** implement command registry, search router, and SQLite FTS engine ([#272](https://github.com/choiceoh/Deneb/issues/272)) ([a85d431](https://github.com/choiceoh/Deneb/commit/a85d431beca53c003817b731751d583f166a134a))

### Bug Fixes

- add backward compatibility for OPENCLAW\_\* env vars and ~/.openclaw/ path ([d21d216](https://github.com/choiceoh/Deneb/commit/d21d2168941ab5d58084de5b8f2f83ed90cd0016))
- add clean:true to tsdown config to prevent stale dist artifacts breaking rebuilds ([849e8ee](https://github.com/choiceoh/Deneb/commit/849e8ee05b7d86c41c528fc8f4d93d0b4b97c5b0))
- add missing shouldSpawnWithShell export and fix process tool supervisor test mocking ([#146](https://github.com/choiceoh/Deneb/issues/146)) ([c54e081](https://github.com/choiceoh/Deneb/commit/c54e0818452e07c70b0857d669fd1aa247d35476))
- add rolldown plugin to prefer .ts over stale .js in extensions/ ([1f28a29](https://github.com/choiceoh/Deneb/commit/1f28a2915bc43ae789826adeb59d26e7f5954cf1))
- address critical bugs in 4 core modules ([#156](https://github.com/choiceoh/Deneb/issues/156)) ([d3620a7](https://github.com/choiceoh/Deneb/commit/d3620a7b16c3b90c9b8bd52fe63cb67574d0a01f))
- address potential runtime bugs in LCM engine and VegaMemoryManager ([eeb9f5f](https://github.com/choiceoh/Deneb/commit/eeb9f5f6b4f1aa51a6aaaeb9aee29898389c9636))
- auto-clean stale .js files in extensions/ before build ([1f192fa](https://github.com/choiceoh/Deneb/commit/1f192faf4cf4ba0fdb34c4658b6d82b71f51f626))
- autonomous tool schema duplicate enum + goal seeding when no active goals ([#71](https://github.com/choiceoh/Deneb/issues/71)) ([a6bf967](https://github.com/choiceoh/Deneb/commit/a6bf967776f79234e2bdf40235bafdfbf8c41590))
- await floating promises, log silent catch errors in gateway and memory ([8bae89a](https://github.com/choiceoh/Deneb/commit/8bae89a7e2a149eb0c847daea9d8e146978f31b0))
- channel plugin bugs — uninitialized sendPayload result, stuck status reactions, registry cache miss ([#64](https://github.com/choiceoh/Deneb/issues/64)) ([6599981](https://github.com/choiceoh/Deneb/commit/65999819738c58546070239394f4074dd7d83cf9))
- clean up stale references and build artifacts from CLI removal ([#242](https://github.com/choiceoh/Deneb/issues/242)) ([e342d3c](https://github.com/choiceoh/Deneb/commit/e342d3cabddafe87e6de3ad8730e9435a32a87b7))
- clear stale active-run state on SIGUSR1 restart to prevent queued utterances ([69d3bb7](https://github.com/choiceoh/Deneb/commit/69d3bb7eec3a2aa72b457bbc62dad29581b8d46e))
- code review — improve casts, validate role, fix dead code and stub types ([30bfacb](https://github.com/choiceoh/Deneb/commit/30bfacb272e00138d325adfcfbfe02a7ef9570f8))
- complete openclaw→deneb rebrand in wizard, scripts, and test fixtures ([45ece83](https://github.com/choiceoh/Deneb/commit/45ece839855bee656d4132820da3261030fb46cd))
- convert runtime module facades to proper lazy-loading boundaries ([#128](https://github.com/choiceoh/Deneb/issues/128)) ([53faa6d](https://github.com/choiceoh/Deneb/commit/53faa6de11dc42eca2bc20109399100db4ad1077))
- correct payload filter logic and cap debounce retries (PR[#108](https://github.com/choiceoh/Deneb/issues/108) regression) ([#126](https://github.com/choiceoh/Deneb/issues/126)) ([f5475a4](https://github.com/choiceoh/Deneb/commit/f5475a4027af45bd2343ad43557ef215f69b0270))
- ensure typing indicator cleanup on pre-runner errors ([#188](https://github.com/choiceoh/Deneb/issues/188)) ([b01321d](https://github.com/choiceoh/Deneb/commit/b01321d82282e4b32cea1c68345c45be47269e1c))
- explicitly rm -rf dist/ before tsdown build to prevent stale chunk resolution ([02c0cec](https://github.com/choiceoh/Deneb/commit/02c0cec314e522dffb4946e49992f23b0614df1b))
- fail-fast abort in parallel-check, reduce false-positive dependents in dev-affected ([#152](https://github.com/choiceoh/Deneb/issues/152)) ([98fd166](https://github.com/choiceoh/Deneb/commit/98fd1666750fc51ca0bc33224b20ebd612529e13))
- fix release-please config and sync all versions to 3.8.0 ([#254](https://github.com/choiceoh/Deneb/issues/254)) ([55f4696](https://github.com/choiceoh/Deneb/commit/55f4696b3e486f4b3c012aff650fd2f79f808e3f))
- harden path traversal checks and make JSON writes atomic ([#86](https://github.com/choiceoh/Deneb/issues/86)) ([e877594](https://github.com/choiceoh/Deneb/commit/e87759486273aeb627870580c1887490e8e84693))
- improve maintainability with type splits, error handling, and type safety ([#61](https://github.com/choiceoh/Deneb/issues/61)) ([1aa9465](https://github.com/choiceoh/Deneb/commit/1aa9465da92446a1ff5cd192714cc4b96a0ef9e1))
- link @deneb/native as file dependency and remove orphaned native/ ([#271](https://github.com/choiceoh/Deneb/issues/271)) ([95b2d09](https://github.com/choiceoh/Deneb/commit/95b2d0947f0f5aa6c889e3d80fb7d0cf126c4036))
- migrate lossless-claw plugin entry keys into config sub-object ([ee45235](https://github.com/choiceoh/Deneb/commit/ee452352ba4bacb0c785953ca81c598c17ddc413))
- normalize legacy qmd-wrapper.sh command to bare qmd ([bd5b15e](https://github.com/choiceoh/Deneb/commit/bd5b15e33716cee544282b13c7ea57d6958b8c57))
- observer compression ratio hitting leaf-level caps (~3%) instead of targetRatio (10-20%) ([#72](https://github.com/choiceoh/Deneb/issues/72)) ([00d79eb](https://github.com/choiceoh/Deneb/commit/00d79eb88510bc52dfdbf6e4dd31cbf2ecf23845))
- PR[#26](https://github.com/choiceoh/Deneb/issues/26)/[#27](https://github.com/choiceoh/Deneb/issues/27) runtime fixes + v3.5 ([32003a8](https://github.com/choiceoh/Deneb/commit/32003a814c065048a907b65cea076a99996c7bdf))
- preserve allowlist prefix stripping for removed channels + safe contract registry ([c0916e4](https://github.com/choiceoh/Deneb/commit/c0916e40056899f02711e382d93045ffc95e5964))
- preserve Anthropic thinking block order during empty-text filtering, decouple memory-core tool registration ([#127](https://github.com/choiceoh/Deneb/issues/127)) ([04d3a89](https://github.com/choiceoh/Deneb/commit/04d3a899ebc96b179200ee1daec90792219aaa71))
- preserve orphaned user messages for re-queue instead of silently dropping them ([#232](https://github.com/choiceoh/Deneb/issues/232)) ([acd506c](https://github.com/choiceoh/Deneb/commit/acd506cabc7f5ebda367eb3f6e86769c23fcf283))
- prevent agent response cutoff by adding error handling and safety-net to chat finalization ([#131](https://github.com/choiceoh/Deneb/issues/131)) ([91af832](https://github.com/choiceoh/Deneb/commit/91af832e2ff00f2ca4890ea875503d6456b8f583))
- prevent input swallowing and output loss during compaction and active runs ([#108](https://github.com/choiceoh/Deneb/issues/108)) ([ddbb68f](https://github.com/choiceoh/Deneb/commit/ddbb68fe4ccb15ad073a761e300b26dcc5bf5ed5))
- prevent session JSONL bloat degrading response speed and quality ([#66](https://github.com/choiceoh/Deneb/issues/66)) ([d64e6fa](https://github.com/choiceoh/Deneb/commit/d64e6fa030e3270b3438b0559239f95309494f87))
- prevent silent message drops in chat-to-agent delivery pipeline ([#218](https://github.com/choiceoh/Deneb/issues/218)) ([d83245a](https://github.com/choiceoh/Deneb/commit/d83245a6ce3bc621748ca3b57cb2b982e691392b))
- prevent test suite hang from sync jiti compilation of channel-runtime barrel ([#68](https://github.com/choiceoh/Deneb/issues/68)) ([3c5c4ac](https://github.com/choiceoh/Deneb/commit/3c5c4acabb2adb5f12e4a81351c8c7f1d5ab979d))
- remove as-any casts and extract per-channel audit functions for maintainability ([66a0f6f](https://github.com/choiceoh/Deneb/commit/66a0f6f26e63b7d17f1f8279ea99bb016bf6fdfc))
- remove broken jiti integration tests and fix context engine reserved id in loader tests ([#79](https://github.com/choiceoh/Deneb/issues/79)) ([55ecd6e](https://github.com/choiceoh/Deneb/commit/55ecd6ea9c3ffbcd0637a8abb9b147f6c49e2ee6))
- remove orphaned speech plugin-sdk entrypoints breaking build ([#265](https://github.com/choiceoh/Deneb/issues/265)) ([fa6f507](https://github.com/choiceoh/Deneb/commit/fa6f507d99f1b63f5845be8c7c1a35ac0e1ea91d))
- remove stale daemon-cli build entries left after CLI removal ([#234](https://github.com/choiceoh/Deneb/issues/234)) ([#240](https://github.com/choiceoh/Deneb/issues/240)) ([7ac7b87](https://github.com/choiceoh/Deneb/commit/7ac7b879286fdad1d3a024d190afadf59a4bb98b))
- remove stale extension references from CI lint scripts and guard tests ([681123e](https://github.com/choiceoh/Deneb/commit/681123e8550aa165a83343dd50951e47f4506c9f))
- replace as-any casts with proper types, remove dead Discord runtime, guard plugin.config access ([72c6f03](https://github.com/choiceoh/Deneb/commit/72c6f03af589fb52e1d0310c0b59789ebfcedf2c))
- replace stale eslint-disable comments with oxlint-disable and remove unnecessary suppressions ([#138](https://github.com/choiceoh/Deneb/issues/138)) ([8e3ab12](https://github.com/choiceoh/Deneb/commit/8e3ab126bac8c0f2f7d9fb8a0a0c2dc9f7f799c4))
- reset followup queue drain state on SIGUSR1 restart ([fc070c9](https://github.com/choiceoh/Deneb/commit/fc070c926abe49e0b0dc7cf1efa356d30bd55467))
- resolve 4 [@ts-expect-error](https://github.com/ts-expect-error) suppressions for strict mode ([#174](https://github.com/choiceoh/Deneb/issues/174)) ([701d73c](https://github.com/choiceoh/Deneb/commit/701d73c9081ccb0684d1e59687f917c6d5e4438f))
- resolve agent module bugs — security hardening, stale mock paths, missing tool ([#149](https://github.com/choiceoh/Deneb/issues/149)) ([d33a4c8](https://github.com/choiceoh/Deneb/commit/d33a4c80fa6ff95c283dd07d14be9081d200f79a))
- resolve build and test bugs across native addon, markdown, and config ([#264](https://github.com/choiceoh/Deneb/issues/264)) ([a7a824c](https://github.com/choiceoh/Deneb/commit/a7a824c1caa141cb4e2b6cde0f1a5b5e2d916dfc))
- resolve infra module test failures across 9 test files ([#161](https://github.com/choiceoh/Deneb/issues/161)) ([2d2a977](https://github.com/choiceoh/Deneb/commit/2d2a9774b2fbc903ef84ca3b58170f6a760353fe))
- resolve master check errors (missing swift policy script, format, stale plugin-sdk exports) ([#39](https://github.com/choiceoh/Deneb/issues/39)) ([eac92ae](https://github.com/choiceoh/Deneb/commit/eac92aea8163787750e4563bbe65e06266cbba47))
- resolve PR [#184](https://github.com/choiceoh/Deneb/issues/184) merge bugs — missing dep, workspace profiles, CI gaps ([#200](https://github.com/choiceoh/Deneb/issues/200)) ([d0f889e](https://github.com/choiceoh/Deneb/commit/d0f889e1851018cbe28ca730e33d50745223925c))
- resolve PR[#195](https://github.com/choiceoh/Deneb/issues/195) merge bugs ([#197](https://github.com/choiceoh/Deneb/issues/197)) ([9208ead](https://github.com/choiceoh/Deneb/commit/9208ead95c6b9519c98e2dc82ebea733432e4ad1))
- resolve recent merge bugs — duplicate Go map keys, stale provider field, wrong bridge call, highway binary path ([#209](https://github.com/choiceoh/Deneb/issues/209)) ([083b698](https://github.com/choiceoh/Deneb/commit/083b698246a357ff291dd663369dd132024b9bc2))
- resolve remaining stub root causes — inline dead stubs, remove dead telegram pairing migration ([#90](https://github.com/choiceoh/Deneb/issues/90)) ([b168e36](https://github.com/choiceoh/Deneb/commit/b168e361c97658a5a7c74b7edb6b76e06618ed02))
- resolve Rust/Go build errors — missing napi imports, FFI field mismatch, Go test params ([#217](https://github.com/choiceoh/Deneb/issues/217)) ([df5f454](https://github.com/choiceoh/Deneb/commit/df5f454e586989a5d2da3b5a1a581b063e8b92ff))
- resolve stub root causes — delete 18 pure-stub files, inline no-op behavior, fix secret-equal ([#57](https://github.com/choiceoh/Deneb/issues/57)) ([377a95f](https://github.com/choiceoh/Deneb/commit/377a95f81fd2e7aa319a4407c56cf2203e705476))
- resolve technical debt — add error logging to empty catch blocks, eliminate double casts, fix qmd-manager test mock mismatches ([#103](https://github.com/choiceoh/Deneb/issues/103)) ([781c40e](https://github.com/choiceoh/Deneb/commit/781c40e91eaa4df058ddcf1c5eeff69b9d5a1497))
- resolve three bugs in util modules ([#142](https://github.com/choiceoh/Deneb/issues/142)) ([0cec3b3](https://github.com/choiceoh/Deneb/commit/0cec3b3c04af1659083f7efc55c0e9fb1ed6ae58))
- restore device identity crypto and fix gateway test infrastructure ([#150](https://github.com/choiceoh/Deneb/issues/150)) ([a60dc8a](https://github.com/choiceoh/Deneb/commit/a60dc8afb9ff2a576d9da6af12170585d2fdd3a2))
- restore missing pw-session extracted modules and fix env-dependent test ([#58](https://github.com/choiceoh/Deneb/issues/58)) ([91b2b3f](https://github.com/choiceoh/Deneb/commit/91b2b3fc94952395b4359191db718e788996b6cc))
- restore WhatsApp normalize stub (required by outbound-session import) ([b70ee80](https://github.com/choiceoh/Deneb/commit/b70ee80e42fa5ec53ad1b9851dcce3b1fbbe1edb))
- simplify release-please to single package (fix linked-versions crash) ([#261](https://github.com/choiceoh/Deneb/issues/261)) ([11fa93c](https://github.com/choiceoh/Deneb/commit/11fa93cf92e5139e8f38b03c9dbb26761019e37c))
- sync version badges, fix Go bridge race, fix channel-starter types ([#277](https://github.com/choiceoh/Deneb/issues/277)) ([f3d32d9](https://github.com/choiceoh/Deneb/commit/f3d32d9a7022f3c88164088b5f8c075e9dd665cb))
- telegram network test — stub proxy env vars and correct timeout expectation ([#148](https://github.com/choiceoh/Deneb/issues/148)) ([73275dc](https://github.com/choiceoh/Deneb/commit/73275dceae92aebb18f66b0d4d2bdd373ce91e18))
- update package-lock.json with deneb package name and bin entry ([af6037a](https://github.com/choiceoh/Deneb/commit/af6037a2b29111e4699f523989942d0da3119fe5))
- update package.json bin/files/exports to reference deneb.mjs instead of missing openclaw.mjs ([87796e3](https://github.com/choiceoh/Deneb/commit/87796e3de12aaed5f0094103ecad4a8ac37dd780))
- update plugin test fixture archives to use deneb branding ([309840d](https://github.com/choiceoh/Deneb/commit/309840da99bbdfe12a73b0908bd9a694a91a35a6))
- use ctx.hookRunner instead of broken vi.mock for after_tool_call e2e test, add missing successfulCronAdds to test helper ([#145](https://github.com/choiceoh/Deneb/issues/145)) ([fcb7d13](https://github.com/choiceoh/Deneb/commit/fcb7d131711653a38fa6d33d7834a1b3acb5aa60))
- use git add -f in pre-commit hook to handle tracked files in gitignored dirs ([#70](https://github.com/choiceoh/Deneb/issues/70)) ([cc09cb7](https://github.com/choiceoh/Deneb/commit/cc09cb72a9a87fb0fe7919968af6ce3616cbd913))
- use import.meta.dirname instead of process.cwd() for reliable file path resolution ([34a9b44](https://github.com/choiceoh/Deneb/commit/34a9b44c2e461fea511c253c1d3f3ccb237db65f))
- use relative symlinks for dist-runtime extension node_modules ([#144](https://github.com/choiceoh/Deneb/issues/144)) ([9064844](https://github.com/choiceoh/Deneb/commit/90648449c254e08e972dfbe1d6f06a49396334ee))
- v3.150 bugfixes — resolve TS errors, lint, and broken extension references ([3af9c07](https://github.com/choiceoh/Deneb/commit/3af9c07f4427835c62f24769c70b0fd4e01339ec))

## [3.5.7](https://github.com/choiceoh/Deneb/compare/v3.2.0...v3.5.7) (2026-03-23)

프로젝트 문서 전면 최신화.

### Features

- **프로젝트 문서 최신화** — README, VISION, CONTRIBUTING, SECURITY, CHANGELOG를 현재 v3.5.7 기준으로 전면 갱신
- **Node.js 요구사항 통일** — 최소 22.16.0, 권장 Node 24로 전 문서 일치화
- **코드베이스 규모 갱신** — 실측 기준 ~440K LOC 반영
- **개발 커맨드 갱신** — `pnpm check` (oxlint + oxfmt) 기준으로 통일
- **아키텍처 다이어그램 갱신** — 현재 `src/` 디렉토리 구조 반영 (plugin-sdk, routing, tts, web-search, vega)

## [3.2.0](https://github.com/choiceoh/Deneb/compare/v3.0.0...v3.2.0) (2026-03-21)

ACP (Claude Code) 연동 활성화, 코드 구조 리팩토링.

### Features

- **ACP/Claude Code 연동** — acpx 플러그인 활성화, `acp.allowedAgents` 설정
- **Refactor** — 대형 파일에서 9개 전용 모듈 추출 (PR #22)

## [3.0.0](https://github.com/choiceoh/Deneb/releases/tag/v3.0.0) (2026-03-21)

Deneb 최초 릴리스. 독립 프로젝트로 시작.

### Features

- **Aurora Memory Module** — AI-agent-first 메모리 파일 관리 (memory-md-manager)
- **Vega 메모리 백엔드 통합** — VegaMemoryManager, Aurora 네이티브화
- **Vega CLI 래퍼** — bin/vega wrapper + install.sh
- **Aurora Context Engine** — DAG-based compaction, background observer, multi-layer recall
- **컨텍스트 엔진** — transcript maintenance 기능
- **Telegram custom apiRoot** 지원
- Telegram 전용 빌드 (미사용 채널 어댑터 제거)
- 에러 리질리언스 계층 추가
- Rolldown 빌드 안정화 (stale .js 정리, clean:true)
- Subagent 타임아웃 시 부분 진행 결과 포함
- JSONL 세션 로그 트렁케이션 (디스크 과다 사용 방지)
- Compaction 후 세션 JSONL 자동 트렁케이션

### Bug Fixes

- Telegram 스트리밍 미리보기 종료 시 message:sent hook 발행
- Telegram dmPolicy pairing 경고
- 시퀀스 갭 브로드캐스트 스킵
- 잘못된 형식의 replay tool call sanitize
- Thread binding 직렬화
- Telegram accountId 누락 시 잘못된 봇 라우팅 방지
