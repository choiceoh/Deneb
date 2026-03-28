---
summary: "Deneb on Raspberry Pi (budget self-hosted setup)"
read_when:
  - Setting up Deneb on a Raspberry Pi
  - Running Deneb on ARM devices
  - Building a cheap always-on personal AI
title: "Raspberry Pi"
---

# Deneb on Raspberry Pi

## Goal

Run a persistent, always-on Deneb Gateway on a Raspberry Pi for **~$35-80** one-time cost (no monthly fees).

Perfect for:

- 24/7 personal AI assistant
- Home automation hub
- Low-power, always-available Telegram/WhatsApp bot

## Hardware Requirements

| Pi Model | RAM     | Works?  | Notes                     |
| -------- | ------- | ------- | ------------------------- |
| **Pi 5** | 4GB/8GB | ✅ Best | Fastest, recommended      |
| **Pi 4** | 4GB     | ✅ Good | Sweet spot for most users |
| **Pi 4** | 2GB     | ✅ OK   | Works, add swap           |

**Minimum specs:** 2GB RAM, 64-bit ARM (aarch64), 500MB disk
**Recommended:** 4GB+ RAM, 64-bit OS, 16GB+ SD card (or USB SSD)

## What you need

- Raspberry Pi 4 (2GB+) or Pi 5
- MicroSD card (16GB+) or USB SSD (better performance)
- Power supply (official Pi PSU recommended)
- Network connection (Ethernet or WiFi)
- ~30 minutes

## 1) Flash the OS

Use **Raspberry Pi OS Lite (64-bit)** — no desktop needed for a headless server.

1. Download [Raspberry Pi Imager](https://www.raspberrypi.com/software/)
2. Choose OS: **Raspberry Pi OS Lite (64-bit)**
3. Click the gear icon (⚙️) to pre-configure:
   - Set hostname: `gateway-host`
   - Enable SSH
   - Set username/password
   - Configure WiFi (if not using Ethernet)
4. Flash to your SD card / USB drive
5. Insert and boot the Pi

## 2) Connect via SSH

```bash
ssh user@gateway-host
# or use the IP address
ssh user@192.168.x.x
```

## 3) System Setup

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install essential packages
sudo apt install -y git curl build-essential

# Set timezone (important for cron/reminders)
sudo timedatectl set-timezone America/Chicago  # Change to your timezone
```

## 4) Install Node.js 24 (ARM64)

```bash
# Install Node.js via NodeSource
curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash -
sudo apt install -y nodejs

# Verify
node --version  # Should show v24.x.x
npm --version
```

## 5) Add Swap (Important for 2GB or less)

Swap prevents out-of-memory crashes:

```bash
# Create 2GB swap file
sudo fallocate -l 2G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile

# Make permanent
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab

# Optimize for low RAM (reduce swappiness)
echo 'vm.swappiness=10' | sudo tee -a /etc/sysctl.conf
sudo sysctl -p
```

## 6) Install Deneb

### Option A: Standard Install (Recommended)

```bash
curl -fsSL https://deneb.ai/install.sh | bash
```

### Option B: Hackable Install (For tinkering)

```bash
git clone https://github.com/deneb/deneb.git
cd deneb
npm install
npm run build
npm link
```

The hackable install gives you direct access to logs and code — useful for debugging ARM-specific issues.

## 7) Run Onboarding

```bash
deneb onboard --install-daemon
```

Follow the wizard:

1. **Gateway mode:** Local
2. **Auth:** API keys recommended (OAuth can be finicky on headless Pi)
3. **Channels:** Telegram is easiest to start with
4. **Daemon:** Yes (systemd)

## 8) Verify Installation

```bash
# Check status
deneb status

# Check service
sudo systemctl status deneb

# View logs
journalctl -u deneb -f
```

## 9) Access the Deneb Dashboard

Replace `user@gateway-host` with your Pi username and hostname or IP address.

On your computer, ask the Pi to print a fresh dashboard URL:

```bash
ssh user@gateway-host 'deneb dashboard --no-open'
```

The command prints `Dashboard URL:`. Depending on how `gateway.auth.token`
is configured, the URL may be a plain `http://127.0.0.1:18789/` link or one
that includes `#token=...`.

In another terminal on your computer, create the SSH tunnel:

```bash
ssh -N -L 18789:127.0.0.1:18789 user@gateway-host
```

Then open the printed Dashboard URL in your local browser.

If the UI asks for auth, paste the token from `gateway.auth.token`
(or `DENEB_GATEWAY_TOKEN`) into Control UI settings.

For always-on remote access, see [Tailscale](/gateway/tailscale).

---

## Performance Optimizations

### Use a USB SSD (Huge Improvement)

SD cards are slow and wear out. A USB SSD dramatically improves performance:

```bash
# Check if booting from USB
lsblk
```

See [Pi USB boot guide](https://www.raspberrypi.com/documentation/computers/raspberry-pi.html#usb-mass-storage-boot) for setup.

### Speed up CLI startup (module compile cache)

On lower-power Pi hosts, enable Node's module compile cache so repeated CLI runs are faster:

```bash
grep -q 'NODE_COMPILE_CACHE=/var/tmp/deneb-compile-cache' ~/.bashrc || cat >> ~/.bashrc <<'EOF' # pragma: allowlist secret
export NODE_COMPILE_CACHE=/var/tmp/deneb-compile-cache
mkdir -p /var/tmp/deneb-compile-cache
export DENEB_NO_RESPAWN=1
EOF
source ~/.bashrc
```

Notes:

- `NODE_COMPILE_CACHE` speeds up subsequent runs (`status`, `health`, `--help`).
- `/var/tmp` survives reboots better than `/tmp`.
- `DENEB_NO_RESPAWN=1` avoids extra startup cost from CLI self-respawn.
- First run warms the cache; later runs benefit most.

### systemd startup tuning (optional)

If this Pi is mostly running Deneb, add a service drop-in to reduce restart
jitter and keep startup env stable:

```bash
sudo systemctl edit deneb
```

```ini
[Service]
Environment=DENEB_NO_RESPAWN=1
Environment=NODE_COMPILE_CACHE=/var/tmp/deneb-compile-cache
Restart=always
RestartSec=2
TimeoutStartSec=90
```

Then apply:

```bash
sudo systemctl daemon-reload
sudo systemctl restart deneb
```

If possible, keep Deneb state/cache on SSD-backed storage to avoid SD-card
random-I/O bottlenecks during cold starts.

How `Restart=` policies help automated recovery:
[systemd can automate service recovery](https://www.redhat.com/en/blog/systemd-automate-recovery).

### Reduce Memory Usage

```bash
# Disable GPU memory allocation (headless)
echo 'gpu_mem=16' | sudo tee -a /boot/config.txt

# Disable Bluetooth if not needed
sudo systemctl disable bluetooth
```

### Monitor Resources

```bash
# Check memory
free -h

# Check CPU temperature
vcgencmd measure_temp

# Live monitoring
htop
```

---

## ARM64-Specific Notes

### Binary Compatibility

Most Deneb features work on ARM64 (aarch64), but some external binaries may need ARM64 builds:

| Tool               | ARM64 Status | Notes                               |
| ------------------ | ------------ | ----------------------------------- |
| Node.js            | ✅           | Works great                         |
| WhatsApp (Baileys) | ✅           | Pure JS, no issues                  |
| Telegram           | ✅           | Pure JS, no issues                  |
| gog (Gmail CLI)    | ⚠️           | Check for ARM64 release             |
| Chromium (browser) | ✅           | `sudo apt install chromium-browser` |

If a skill fails, check if its binary has an ARM64 build. Many Go/Rust tools do; some don't.

Verify your OS is 64-bit:

```bash
uname -m
# Must show: aarch64
```

---

## Recommended Model Setup

Since the Pi is just the Gateway (models run in the cloud), use API-based models:

```json
{
  "agents": {
    "defaults": {
      "model": {
        "primary": "anthropic/claude-sonnet-4-20250514",
        "fallbacks": ["openai/gpt-4o-mini"]
      }
    }
  }
}
```

**Don't try to run local LLMs on a Pi** — even small models are too slow. Let Claude/GPT do the heavy lifting.

---

## Auto-Start on Boot

Onboarding sets this up, but to verify:

```bash
# Check service is enabled
sudo systemctl is-enabled deneb

# Enable if not
sudo systemctl enable deneb

# Start on boot
sudo systemctl start deneb
```

---

## Troubleshooting

### Out of Memory (OOM)

```bash
# Check memory
free -h

# Add more swap (see Step 5)
# Or reduce services running on the Pi
```

### Slow Performance

- Use USB SSD instead of SD card
- Disable unused services: `sudo systemctl disable cups bluetooth avahi-daemon`
- Check CPU throttling: `vcgencmd get_throttled` (should return `0x0`)

### Service will not start

```bash
# Check logs
journalctl -u deneb --no-pager -n 100

# Common fix: rebuild
cd ~/deneb  # if using hackable install
npm run build
sudo systemctl restart deneb
```

### ARM64 Binary Issues

If a skill fails with "exec format error":

1. Check if the binary has an ARM64 (aarch64) build
2. Try building from source
3. Or use a Docker container with ARM64 support

### WiFi Drops

For headless Pis on WiFi:

```bash
# Disable WiFi power management
sudo iwconfig wlan0 power off

# Make permanent
echo 'wireless-power off' | sudo tee -a /etc/network/interfaces
```

---

## Cost Comparison

| Setup          | One-Time Cost | Monthly Cost | Notes            |
| -------------- | ------------- | ------------ | ---------------- |
| **Pi 4 (4GB)** | ~$55          | $0           | Recommended      |
| **Pi 5 (4GB)** | ~$60          | $0           | Best performance |
| **Pi 5 (8GB)** | ~$80          | $0           | Future-proof     |
| DigitalOcean   | $0            | $6/mo        | $72/year         |
| Hetzner        | $0            | €3.79/mo     | ~$50/year        |

**Break-even:** A Pi pays for itself in ~6-12 months vs cloud VPS.

---

## See Also

- [Getting Started](/start/getting-started) — installation guide
- [Hetzner guide](/install/hetzner) — Docker setup
- [Tailscale](/gateway/tailscale) — remote access
- [Nodes](/nodes) — pair your laptop/phone with the Pi gateway
