# Deneb 폰 연동 (phone ↔ gateway over SSH)

스마트폰(Termux)에서 발생한 이벤트 — 알림·위치·클립보드 — 를 SSH 터널로 Deneb
게이트웨이의 `POST /api/event/ingest` 로 보내, 비서실장 능동판정 턴을 돌린다.
알릴 가치가 있으면 네이티브 업무 피드에 카드+푸시로 뜨고, 광고·OTP 같은 노이즈는
서버에서 억제된다. **폰은 전달만, 판단은 게이트웨이가 한다.**

> 서버 측 엔드포인트는 `gateway-go/internal/runtime/server/server_http_event_ingest.go`.
> cron/gmail-poll 과 똑같이 `SendSync → relayNative` 능동 발화 경로를 재사용한다.

## 구성 요소

| 파일 | 역할 |
|---|---|
| `deneb-tunnel` | autossh 상시 SSH 터널. 폰 `localhost:18789` → 게이트웨이 호스트 loopback `18789` |
| `deneb-emit`   | 이벤트 1건을 `/api/event/ingest` 로 POST (위 터널 경유) |

**인증:** ingest 엔드포인트는 **loopback 전용**이다. SSH 세션으로 포워드된 요청은
호스트 입장에서 loopback 으로 도착하므로 게이트웨이 토큰이 필요 없다 — **SSH 키를
쥐고 있는 것 자체가 인증**이다.

## 1회 설정 (폰 Termux)

### 1) Termux + 패키지
F-Droid 의 Termux 설치 후:
```bash
pkg update && pkg install -y openssh autossh jq termux-api
```
`termux-api` 는 클립보드·위치 등 Phase 2/3 용 (Termux:API 앱도 F-Droid 에서 함께 설치).

### 2) SSH 키 생성 + 게이트웨이 등록
```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N ""
cat ~/.ssh/id_ed25519.pub     # ← 이 공개키를 게이트웨이 호스트의
                              #   ~/.ssh/authorized_keys 에 추가
```

### 3) `~/.ssh/config` 에 호스트 alias
게이트웨이 호스트(예: Tailscale IP)를 `deneb-host` 로 별칭한다:
```
Host deneb-host
    HostName <gateway-host>       # 예: 100.x.x.x (Tailscale) 또는 LAN IP
    User <your-user>
    IdentityFile ~/.ssh/id_ed25519
    ServerAliveInterval 30
```

### 4) 스크립트 배치
이 디렉토리의 `deneb-tunnel`, `deneb-emit` 을 폰의 `~/bin` (PATH 위)에 복사하고 실행권한 부여:
```bash
mkdir -p ~/bin && cp deneb-tunnel deneb-emit ~/bin/ && chmod +x ~/bin/deneb-*
echo 'export PATH="$HOME/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
```

## 터널 켜기 + 부팅 영속

수동:
```bash
deneb-tunnel &        # 백그라운드 상시 터널
```

부팅 시 자동 (권장) — **Termux:Boot** 앱 설치 후:
```bash
mkdir -p ~/.termux/boot
cat > ~/.termux/boot/deneb-tunnel <<'EOF'
#!/data/data/com.termux/files/usr/bin/bash
termux-wake-lock
exec ~/bin/deneb-tunnel
EOF
chmod +x ~/.termux/boot/deneb-tunnel
```

## 동작 확인
터널이 떠 있는 상태에서:
```bash
# actionable — 잠시 후 업무 피드에 카드/푸시가 떠야 한다
deneb-emit notification "테스트: 내일 3시 미팅 가능?" 카카오톡
# → {"status":"accepted"}

# noise — accepted 지만 서버가 NO_REPLY 로 판정해 카드/푸시가 뜨지 않아야 정상
deneb-emit notification "[Web발신] 인증번호 [123456]" 문자
```

## 이벤트 소스 연결

`deneb-emit` 은 전송 도관일 뿐이다 — 폰의 실제 이벤트를 여기에 물려야 능동형이 산다.

### WiFi 컨텍스트 — `deneb-context-watch` (제공됨, 순수 Termux)

WiFi SSID 변화를 감지해 `context` 이벤트로 보낸다. 게이트웨이가 출근/퇴근 타이밍을
잡아 브리핑한다(회사 접속=출근 → 오늘 일정·우선순위, 집 접속=퇴근 → 하루 마감 요약).

1. SSID→라벨 매핑:
   ```bash
   cat > ~/.deneb-context.conf <<'EOF'
   TopSolar-5G=회사
   home-wifi=집
   EOF
   ```
2. 배치 + 부팅 영속(`deneb-tunnel` 과 함께 백그라운드 실행):
   ```bash
   cp deneb-context-watch ~/bin/ && chmod +x ~/bin/deneb-context-watch
   # ~/.termux/boot/deneb-tunnel 끝에 다음 줄 추가:
   #   ~/bin/deneb-context-watch &
   ```
3. `termux-wifi-connectioninfo` 는 **위치 권한**이 필요하다(없으면 SSID 가
   `<unknown ssid>`). Termux:API 앱에 위치 권한을 허용한다.

> 시작 시점 네트워크는 baseline 으로만 잡고 알리지 않으며, SSID *변화* 때만 보낸다.
> 그 전환이 브리핑할 가치인지는 게이트웨이가 판단한다(noise floor).

### 알림 — Tasker / AutoNotification (사용자 설정, 가치 최고)

Termux 단독으로는 타 앱 알림을 못 읽는다(NotificationListenerService 권한). **Tasker +
AutoNotification**(또는 MacroDroid)으로 잡아 액션에서 `deneb-emit` 을 호출한다:

1. **Profile**: AutoNotification Intercept — 앱 필터에 카카오톡·메시지 등 **원하는 앱만**.
2. **Task** → Run Shell:
   ```
   ~/bin/deneb-emit notification "%antitle: %antext" "%anappname"
   ```
   (PATH 를 못 잡으면 절대경로
   `/data/data/com.termux/files/usr/bin/bash ~/bin/deneb-emit …` 사용.)
3. 민감 앱(은행·인증)은 Profile 필터에서 **제외**한다. 서버 noise floor 가
   비-actionable(OTP·광고)을 억제하지만, 전송 자체를 막는 게 더 안전하다.

### 클립보드 — 공유(share) 기반 권장

클립보드 **자동 폴링 watcher 는 제공하지 않는다** — 비밀번호·OTP·카드번호가 전부
서버로 흐르기 때문. 필요한 것만 명시적으로 보낸다:
```bash
# 회의록·카톡 대화 등 캡처가 필요할 때만 수동으로
termux-clipboard-get | deneb-emit clipboard - 클립보드
```
또는 안드로이드 공유 메뉴 → Termux:Tasker 로 "선택한 텍스트만" 보내는 share 액션을
구성한다(전수 폴링보다 안전).

## 보안 메모
- SSH 키 인증 + loopback 게이트웨이 = 게이트웨이에 새로운 노출이 0.
- 알림 본문이 SSH 로 흐른다(금융·인증 포함). 비-actionable 은 서버가 억제하지만,
  민감 소스(은행 앱 등)는 Tasker 단에서 아예 제외하거나, `source` 라벨로 게이트웨이
  필터를 강화하는 편이 안전하다.

## 오프라인 큐잉

폰은 이동 중 터널이 끊긴다(지하철·WiFi↔셀룰러 전환). 이때 `deneb-emit` 은 이벤트를
잃지 않고 `~/.deneb-queue` 에 저장하고, **다음 `deneb-emit` 호출이 백로그를 오래된
것부터 먼저 보낸 뒤** 자기 이벤트를 전송한다 — 연결이 끊겨도 순서대로 따라잡는다.

이벤트가 한동안 없으면 큐가 안 비워지니, 주기적으로 배수만 돌린다:
```bash
deneb-emit --flush     # 새 이벤트 없이 큐만 비운다
```
`termux-job-scheduler` 로 등록해 두면 자동 배수(예: 5분마다). 인자를 못 주므로 한 줄
래퍼를 둔다:
```bash
printf '#!/data/data/com.termux/files/usr/bin/bash\nexec ~/bin/deneb-emit --flush\n' > ~/bin/deneb-flush
chmod +x ~/bin/deneb-flush
termux-job-scheduler --script ~/bin/deneb-flush --period-ms 300000
```
큐 위치는 `DENEB_QUEUE_DIR` 로 바꿀 수 있다(기본 `~/.deneb-queue`).
