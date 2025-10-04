# First-Time Setup (Ubuntu/Debian + VS Code + Codex)

This guide gets you coding Grimnir Radio on Ubuntu/Debian with VS Code. It includes Go tooling, dependencies, cloning the repo from Friends In Code, and recommended IDE extensions.

## 1) System prerequisites

Run on Ubuntu 22.04/24.04 or Debian 12.

```
sudo apt update
sudo apt install -y git curl unzip build-essential pkg-config \
  ffmpeg sqlite3 \
  gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
  gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav
```

## 2) Install Go (1.22+)

Option A: Official tarball (recommended for latest Go). Replace VERSION as needed.
```
GO_VERSION=1.22.6
curl -LO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.profile
source ~/.profile
```

Verify:
```
go version
```

## 3) Install VS Code

```
sudo apt install -y wget gpg apt-transport-https
wget -qO- https://packages.microsoft.com/keys/microsoft.asc | gpg --dearmor | sudo tee /usr/share/keyrings/ms_vscode.gpg >/dev/null
echo "deb [arch=amd64,arm64 signed-by=/usr/share/keyrings/ms_vscode.gpg] https://packages.microsoft.com/repos/code stable main" | sudo tee /etc/apt/sources.list.d/vscode.list
sudo apt update
sudo apt install -y code
```

## 4) Secure SSH setup for GitHub (Linux)

Generate a modern SSH key (ed25519), add it to your agent, and register it in GitHub.

1) Generate key (with email label and strong KDF):
```
ssh-keygen -t ed25519 -a 100 -C "your_email@example.com"
```
Accept default path (`~/.ssh/id_ed25519`) and set a passphrase.

2) Start ssh-agent and add the key:
```
eval "$(ssh-agent -s)"
ssh-add ~/.ssh/id_ed25519
```

3) Copy the public key and add to GitHub:
```
cat ~/.ssh/id_ed25519.pub
# or: xclip -sel clip < ~/.ssh/id_ed25519.pub
```
GitHub → Settings → SSH and GPG keys → New SSH key → paste.

4) Test the connection:
```
ssh -T git@github.com
```
You should see a success message (may ask to trust GitHub's host key on first connect).

5) Use SSH for this repo:
```
# If you already cloned via HTTPS and want to switch:
git remote set-url origin git@github.com:friendsincode/grimnir_radio.git
```

Notes
- Use SSH (not rsh). ed25519 keys are secure and fast; `-a 100` strengthens passphrase hashing.
- Keep your private key permissions restricted (`chmod 600 ~/.ssh/id_ed25519`).

## 5) Clone the repo (Friends In Code)

```
# Recommended (SSH)
git clone git@github.com:friendsincode/grimnir_radio.git
# Alternative (HTTPS)
# git clone https://github.com/friendsincode/grimnir_radio
cd grimnir_radio
```

## 6) VS Code extensions (Go + coding assistant)

Open VS Code in this folder and install the recommended extensions when prompted (we include `.vscode/extensions.json`). Or install via CLI:
```
code --install-extension golang.go
code --install-extension continue.continue
code --install-extension ms-vscode.makefile-tools
code --install-extension humao.rest-client
code --install-extension eamodio.gitlens
```

- Go: official Go support (linting, tools, test UI, debug)
- Continue: in‑editor coding assistant (works with local/remote models). Use your preferred provider if not Codex CLI.
- Makefile Tools: surfaces Make targets in VS Code
- REST Client: quick API calls from `.http` files
- GitLens: enhanced Git history/annotation

Note: If you are using Codex CLI separately, you can run it in the integrated terminal. Continue is optional.

## 7) Environment setup

Copy the example env and adjust values as needed:
```
cp .env.example .env
```
Typical local quick start (SQLite):
- `GRIMNIR_DB_BACKEND=sqlite`
- `GRIMNIR_DB_DSN=file:dev.sqlite?_foreign_keys=on`

## 8) Build, test, run

We include a Makefile and VS Code tasks.

CLI:
```
make verify   # tidy, fmt, vet, (lint if installed), test
make build    # builds ./grimnirradio
```

VS Code:
- Run task “Verify” or “Build” from the command palette
- Start debugger with “Run Grimnir Radio” (loads env from `.env`)

Manual run:
```
source .env
./grimnirradio
```

Health checks:
- HTTP health: http://localhost:8080/healthz
- API health:  http://localhost:8080/api/v1/health
- Metrics:     http://localhost:8080/metrics

## 9) Optional: Database choices

- SQLite (default for dev): no extra setup needed
- Postgres (recommended for prod): `sudo apt install -y postgresql`
- MySQL/MariaDB (optional): `sudo apt install -y mariadb-server`

Set `GRIMNIR_DB_BACKEND` and `GRIMNIR_DB_DSN` accordingly.

## 10) Optional: Codex CLI / agent

If you use Codex CLI, launch it from the VS Code integrated terminal in this repo and point it at your workspace. Configure approvals and network settings per your environment. Continue extension is an optional in‑editor assistant if you prefer not to run a separate CLI.

## 11) Next steps

- Read docs/specs for product, engineering, and programmer details
- Try make verify/build, then hit the health endpoints
- Configure webstreams and try a basic schedule once DB is connected

## 12) Kickstart with AI (prompts)

Use these prompts with your coding assistant (e.g., Continue in VS Code or Codex CLI) to quickly orient and plan work:

- "What state is the code in? Do a quick repo scan and summarize key modules, build status, and any obvious gaps."
- "Create me a task list of things needed to get this project going (build, run, docs, minimal features), ordered by impact."

Tip: Run `make verify` and paste any failures into the chat for targeted fixes.

 
