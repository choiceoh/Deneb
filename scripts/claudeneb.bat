@echo off
REM Claudeneb — Claude Desktop + Deneb (Windows)
REM
REM Launches Claude Desktop connected to Deneb gateway on DGX Spark.
REM Uses OpenAI-compatible backend (local AI/z.ai) — no Anthropic key needed.
REM
REM Setup:
REM   1. Install Claude Desktop for Windows
REM   2. Open SSH tunnel: ssh -L 18789:localhost:18789 -L 30000:localhost:30000 choiceoh@dgx
REM   3. Double-click this file
REM
REM Or set DENEB_HOST to DGX Spark IP for direct connection (no tunnel).

if not defined DENEB_HOST set DENEB_HOST=127.0.0.1

set ANTHROPIC_BASE_URL=http://%DENEB_HOST%:18789
set CLAUDENEB_OPENAI_URL=http://%DENEB_HOST%:30000/v1

REM Override model name if needed (e.g., Qwen, GLM).
REM set CLAUDENEB_MODEL=Qwen3.5-35B-A3B

REM Try common Claude Desktop install paths.
if exist "%LocalAppData%\AnthropicClaude\claude.exe" (
    start "" "%LocalAppData%\AnthropicClaude\claude.exe"
    goto :eof
)
if exist "%LocalAppData%\Programs\claude-desktop\Claude.exe" (
    start "" "%LocalAppData%\Programs\claude-desktop\Claude.exe"
    goto :eof
)
if exist "C:\Program Files\Claude\Claude.exe" (
    start "" "C:\Program Files\Claude\Claude.exe"
    goto :eof
)

echo Claude Desktop not found. Install from https://claude.ai/download
pause
