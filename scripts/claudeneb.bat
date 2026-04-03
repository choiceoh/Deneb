@echo off
REM Claudeneb — Claude Desktop + Deneb (Windows)
REM
REM 1. SSH 터널로 DGX의 Deneb 프록시 연결
REM 2. ANTHROPIC_BASE_URL을 Deneb로 지정
REM 3. Claude Desktop 실행
REM 4. 앱 안에서 SSH remote (100.105.145.6) 선택하면 DGX 파일 접근
REM
REM 사전 조건: SSH 키가 설정되어 있어야 함 (ssh choiceoh@100.105.145.6 접속 가능)

set DGX_HOST=100.105.145.6
set DGX_USER=choiceoh
set DENEB_PORT=18789

REM SSH 터널 열기 (백그라운드). Deneb 프록시 포트만 포워딩.
REM Claude Desktop의 SSH remote는 앱이 자체적으로 SSH 연결함 — 별도 터널 불필요.
echo Claudeneb - Opening SSH tunnel to DGX Spark...
start /b ssh -N -L %DENEB_PORT%:localhost:%DENEB_PORT% %DGX_USER%@%DGX_HOST% 2>nul

REM 터널 연결 대기
timeout /t 2 /nobreak >nul

REM Deneb 프록시를 LLM 백엔드로 설정
set ANTHROPIC_BASE_URL=http://127.0.0.1:%DENEB_PORT%
set CLAUDENEB_OPENAI_URL=http://127.0.0.1:30000/v1

echo Starting Claude Desktop...
echo   API: %ANTHROPIC_BASE_URL% (via SSH tunnel)
echo   After launch, select SSH remote "100.105.145.6" for DGX file access.
echo.

REM Claude Desktop 실행
if exist "%LocalAppData%\AnthropicClaude\claude.exe" (
    start "" "%LocalAppData%\AnthropicClaude\claude.exe"
    goto :eof
)
if exist "%LocalAppData%\Programs\claude-desktop\Claude.exe" (
    start "" "%LocalAppData%\Programs\claude-desktop\Claude.exe"
    goto :eof
)
if exist "%ProgramFiles%\Claude\Claude.exe" (
    start "" "%ProgramFiles%\Claude\Claude.exe"
    goto :eof
)

echo Claude Desktop not found. Install from https://claude.ai/download
pause
