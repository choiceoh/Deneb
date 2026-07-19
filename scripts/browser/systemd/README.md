# 브라우저 사이드카 systemd user 유닛

상주 실브라우저(browse 도구 백엔드)를 부팅·크래시 자동재시작으로 운영한다.

## 설치 (srv4, choiceoh user)

```bash
cp scripts/browser/systemd/*.service ~/.config/systemd/user/
# ExecStart의 node 절대경로를 호스트에 맞게 확인 (기본: node-sdk aarch64)
#   which node → readlink -f 로 실경로 확인, ~/.local/bin 심링크 shadow 주의
cd ~/deneb/scripts/browser && npm install   # runtime 의존성 (node_modules는 gitignore)
loginctl enable-linger choiceoh              # 부팅 시 user 유닛 기동 (이미 on)
systemctl --user daemon-reload
systemctl --user enable --now deneb-browser.service   # xvfb는 Requires로 함께
```

## 구조
- `deneb-browser-xvfb.service` — 가상 X 디스플레이 `:98`
- `deneb-browser.service` — Playwright 헤드풀 크롬 사이드카(:18930), `Requires` xvfb
- noVNC 로그인/조작: `scripts/browser/start-browser-sidecar.sh view`
