---
summary: "Install Deneb — installer script, npm/pnpm, from source, Docker, and more"
read_when:
  - You need an install method other than the Getting Started quickstart
  - You want to deploy to a cloud platform
  - You need to update, migrate, or uninstall
title: "Install"
---

# Install

Already followed [Getting Started](/start/getting-started)? You're all set — this page is for alternative install methods, platform-specific instructions, and maintenance.

## System requirements

- **[Node 24 (recommended)](/install/node)** (Node 22 LTS, currently `22.16+`, is still supported for compatibility; the [installer script](#install-methods) will install Node 24 if missing)
- macOS, Linux, or Windows
- `pnpm` only if you build from source

<Note>
On Windows, we strongly recommend running Deneb under [WSL2](https://learn.microsoft.com/en-us/windows/wsl/install).
</Note>

## Install methods

<Tip>
The **installer script** is the recommended way to install Deneb. It handles Node detection, installation, and onboarding in one step.
</Tip>

<Warning>
For VPS/cloud hosts, avoid third-party "1-click" marketplace images when possible. Prefer a clean base OS image (for example Ubuntu LTS), then install Deneb yourself with the installer script.
</Warning>

<AccordionGroup>
  <Accordion title="Installer script" icon="rocket" defaultOpen>
    Downloads the CLI, installs it globally via npm, and launches onboarding.

    <Tabs>
      <Tab title="macOS / Linux / WSL2">
        ```bash
        curl -fsSL https://deneb.ai/install.sh | bash
        ```
      </Tab>
      <Tab title="Windows (PowerShell)">
        ```powershell
        iwr -useb https://deneb.ai/install.ps1 | iex
        ```
      </Tab>
    </Tabs>

    That's it — the script handles Node detection, installation, and onboarding.

    To skip onboarding and just install the binary:

    <Tabs>
      <Tab title="macOS / Linux / WSL2">
        ```bash
        curl -fsSL https://deneb.ai/install.sh | bash -s -- --no-onboard
        ```
      </Tab>
      <Tab title="Windows (PowerShell)">
        ```powershell
        & ([scriptblock]::Create((iwr -useb https://deneb.ai/install.ps1))) -NoOnboard
        ```
      </Tab>
    </Tabs>

    For all flags, env vars, and CI/automation options, see [Installer internals](/install/installer).

  </Accordion>

  <Accordion title="npm / pnpm" icon="package">
    If you already manage Node yourself, we recommend Node 24. Deneb still supports Node 22 LTS, currently `22.16+`, for compatibility:

    <Tabs>
      <Tab title="npm">
        ```bash
        npm install -g deneb@latest
        deneb onboard --install-daemon
        ```

        <Accordion title="sharp build errors?">
          If you have libvips installed globally (common on macOS via Homebrew) and `sharp` fails, force prebuilt binaries:

          ```bash
          SHARP_IGNORE_GLOBAL_LIBVIPS=1 npm install -g deneb@latest
          ```

          If you see `sharp: Please add node-gyp to your dependencies`, either install build tooling (macOS: Xcode CLT + `npm install -g node-gyp`) or use the env var above.
        </Accordion>
      </Tab>
      <Tab title="pnpm">
        ```bash
        pnpm add -g deneb@latest
        pnpm approve-builds -g        # approve deneb, node-llama-cpp, sharp, etc.
        deneb onboard --install-daemon
        ```

        <Note>
        pnpm requires explicit approval for packages with build scripts. After the first install shows the "Ignored build scripts" warning, run `pnpm approve-builds -g` and select the listed packages.
        </Note>
      </Tab>
    </Tabs>

    Want the current GitHub `main` head with a package-manager install?

    ```bash
    npm install -g github:deneb/deneb#main
    ```

    ```bash
    pnpm add -g github:deneb/deneb#main
    ```

  </Accordion>

  <Accordion title="From source" icon="github">
    For contributors or anyone who wants to run from a local checkout.

    <Steps>
      <Step title="Clone and build">
        Clone the [Deneb repo](https://github.com/deneb/deneb) and build:

        ```bash
        git clone https://github.com/deneb/deneb.git
        cd deneb
        pnpm install
        pnpm ui:build
        pnpm build
        ```
      </Step>
      <Step title="Link the CLI">
        Make the `deneb` command available globally:

        ```bash
        pnpm link --global
        ```

        Alternatively, skip the link and run commands via `pnpm deneb ...` from inside the repo.
      </Step>
      <Step title="Run onboarding">
        ```bash
        deneb onboard --install-daemon
        ```
      </Step>
    </Steps>

    For deeper development workflows, see [Setup](/start/setup).

  </Accordion>
</AccordionGroup>


## After install

Verify everything is working:

```bash
deneb doctor         # check for config issues
deneb status         # gateway status
deneb dashboard      # open the browser UI
```

If you need custom runtime paths, use:

- `DENEB_HOME` for home-directory based internal paths
- `DENEB_STATE_DIR` for mutable state location
- `DENEB_CONFIG_PATH` for config file location

See [Environment vars](/help/environment) for precedence and full details.

## Troubleshooting: `deneb` not found

<Accordion title="PATH diagnosis and fix">
  Quick diagnosis:

```bash
node -v
npm -v
npm prefix -g
echo "$PATH"
```

If `$(npm prefix -g)/bin` (macOS/Linux) or `$(npm prefix -g)` (Windows) is **not** in your `$PATH`, your shell can't find global npm binaries (including `deneb`).

Fix — add it to your shell startup file (`~/.zshrc` or `~/.bashrc`):

```bash
export PATH="$(npm prefix -g)/bin:$PATH"
```

On Windows, add the output of `npm prefix -g` to your PATH.

Then open a new terminal (or `rehash` in zsh / `hash -r` in bash).
</Accordion>

## Update / uninstall

<CardGroup cols={3}>
  <Card title="Updating" href="/install/updating" icon="refresh-cw">
    Keep Deneb up to date.
  </Card>
  <Card title="Migrating" href="/install/migrating" icon="arrow-right">
    Move to a new machine.
  </Card>
  <Card title="Uninstall" href="/install/uninstall" icon="trash-2">
    Remove Deneb completely.
  </Card>
</CardGroup>
