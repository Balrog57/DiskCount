# DiskCount

DiskCount is a Telegram bot that watches HDD/SSD deals and notifies you when an alert matches your filters.

Project tracking files:

- `PLAN.md`: implementation plan.
- `DASHBOARD.md`: current realization dashboard.

Maintenance rule: every functional, deployment, or configuration change must update the relevant Markdown tracking files and be pushed to the private GitHub repository.

The scanner is intentionally conservative:

- DiskPrices France is parsed from its public table.
- PricePerGig is consumed through its public JSON API for `amazon.fr`.
- PricePerTB France is parsed from its public table, with the same headless fallback when direct HTTP cannot read it.
- Dealabs is consumed through RSS alert feeds that you configure.
- Keepa is optional and only queried through its API when a key and ASIN list are configured.
- eBay is queried through the official Browse API when credentials are configured.
- Idealo and leDenicheur can consume configured feeds and configured page URLs. Page URLs use normal HTTP first,
  then optional Playwright headless rendering for public pages that require JavaScript.
- leboncoin is consumed through configured alert/feed URLs.

## Quick start

```powershell
python -m venv .venv
.\.venv\Scripts\python -m pip install -e ".[dev]"
copy deploy\diskcount.env.example .env
notepad .env
.\.venv\Scripts\python -m diskcount init-db
.\.venv\Scripts\python -m diskcount check
.\.venv\Scripts\python -m diskcount list --min-tb 16 --max-eur-tb 20 --media rotational
.\.venv\Scripts\python -m diskcount run
```

On Debian the same app is intended to run under `systemd`; see `deploy/diskcount.service`.

## Configuration

Environment variables:

- `TELEGRAM_BOT_TOKEN`: token created with BotFather.
- `TELEGRAM_ADMIN_USER_IDS`: comma-separated Telegram user IDs with admin rights. Set your own ID here on the VPS.
- `TELEGRAM_ALLOWED_USER_IDS`: optional static comma-separated Telegram user IDs allowed to control the bot. Dynamic users are managed from Telegram and stored in SQLite.
- `TELEGRAM_POLLING_TIMEOUT_SECONDS`: default `2`, short long-poll timeout for stable restarts.
- `DATABASE_URL`: default `sqlite:///./diskcount.sqlite3`; Debian example uses `/var/lib/diskcount/diskcount.sqlite3`.
- `DISKPRICES_URL`: default `https://diskprices.com/?locale=fr`.
- `PRICEPERGIG_ENABLED`: default `true`.
- `PRICEPERGIG_API_URL`: default `https://api.pricepergig.com/drives`.
- `PRICEPERGIG_MARKETPLACE`: default `amazon.fr`.
- `PRICEPERGIG_MAX_RESULTS`: default `200`.
- `PRICEPERTB_URLS`: default `https://pricepertb.com/fr`.
- `DEALABS_RSS_URLS`: comma-separated RSS alert URLs from Dealabs.
- `IDEALO_FEED_URLS`: comma-separated feed or alert URLs for Idealo-compatible entries.
- `IDEALO_PAGE_URLS`: comma-separated public Idealo page URLs to parse, with optional headless fallback.
- `LEDENICHEUR_FEED_URLS`: comma-separated feed or alert URLs for leDenicheur-compatible entries.
- `LEDENICHEUR_PAGE_URLS`: comma-separated public leDenicheur page URLs to parse, with optional headless fallback.
- `LEBONCOIN_FEED_URLS`: comma-separated feed or alert URLs for leboncoin-compatible entries.
- `SOURCE_HEADLESS_FALLBACK`: default `true`; requires Playwright and a Chromium browser install.
- `EBAY_CLIENT_ID`, `EBAY_CLIENT_SECRET`: official eBay developer credentials.
- `EBAY_SEARCH_QUERIES`: comma-separated eBay Browse API searches, for example `disque dur 16 To HDD,disque dur 18 To HDD`.
- `EBAY_MARKETPLACE_ID`: default `EBAY_FR`.
- `EBAY_CATEGORY_IDS`: optional comma-separated eBay category IDs.
- `KEEPA_API_KEY`: optional Keepa API key.
- `KEEPA_ASINS`: optional comma-separated ASINs to query through Keepa.
- `POLL_INTERVAL_SECONDS`: default `14400` (4 hours).
- `SCHEDULER_INITIAL_DELAY_SECONDS`: default `30`, lets Telegram polling establish before the first startup scan.
- `TELEGRAM_MESSAGE_DELAY_SECONDS`: default `0.5`, used to pace Telegram notifications.

For headless page rendering on Debian or a fresh Windows environment, install the browser once:

```powershell
python -m playwright install chromium
```

## Telegram UX

The main Telegram interface is clickable:

- `/start`, `/menu`, `/help` open the inline home menu.
- Home tiles: `Creer une alerte`, `Mes alertes`, `Scanner/Test`, `Aide`, and `Admin` for admin users.
- `/create` starts a full alert wizard with inline buttons. Each step edits the same message when Telegram allows it.
- Every wizard and edit screen keeps `Precedent` and `Accueil` at the bottom.
- `Mes alertes` shows each alert as a tile. Opening an alert lets you edit type, condition, capacity, price, DiskPrices categories, connections, pause/resume, and delete with confirmation.
- Admin users get tiles for `Utilisateurs`, `Ajouter`, `Revoquer`, and `Reactiver`. `Ajouter` asks for one controlled text reply: `id nom custom`.

Creation presets:

- SSD capacity, multi-select: `<256 Go`, `~256 Go`, `~512 Go`, `~1 To`, `~2 To`, `~4 To`, `>4 To`, or `Toute capacite`.
- HDD capacity, multi-select: `<4 To`, `4-8 To`, `8-12 To`, `12-16 To`, `16-20 To`, `20-24 To`, `24-30 To`, `>30 To`, or `Toute capacite`.
- HDD price: `<=15`, `<=18`, `<=20`, `<=22`, `<=25` EUR/To, or `Aucune limite`.
- SSD price: `<=0.04`, `<=0.06`, `<=0.08`, `<=0.10`, `<=0.12` EUR/Go, or `Aucune limite`. Internally, SSD thresholds are still stored in EUR/To.
- DiskPrices categories: `External 3.5`, `External 2.5`, `Internal 3.5`, `Internal 2.5`, `Internal Hybrid`, `Internal SAS`, `External SSD`, `Internal SSD`, `M.2 SATA`, `M.2 NVMe`, `U.2/U.3`.
- Connections: `SATA`, `SAS`, `NVMe`, `USB`.
- Sources are backend configuration only. They do not appear in alert creation or editing.

Help guide:

- `Aide` contains a complete tile guide for creation, alert management, capacity presets, prices, DiskPrices categories, connections, scanner/test, admin actions, backend sources, command shortcuts, and advanced text filters.
- Guide screens use the same `Precedent` and `Accueil` navigation as the rest of the bot.

Text commands remain as advanced fallbacks.

Create an alert by text:

```text
/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used discount=5 cooldown=24
```

SSD example with price per GB:

```text
/add name=SSD min_tb=2 max_eur_gb=0.08 media=solid_state condition=new category=m2_nvme interface=nvme
```

Useful commands:

- Type `/` in Telegram to open the command menu with descriptions.
- `/menu` opens the inline tile navigation.
- `/start`, `/menu`, and `/help` remove the old persistent keyboard and show inline tiles under the message.
- `/create` starts the clickable alert creation wizard.
- Tile categories: `Creer une alerte`, `Mes alertes`, `Scanner/Test`, `Aide`, and `Admin` for admin users.
- Submenus always include `Precedent` and `Accueil` at the bottom.
- `/alerts` lists alerts.
- From `/alerts`, each alert opens a clickable editor.
- `/pause 1` disables an alert.
- `/resume 1` enables it again.
- `/delete 1` removes it.
- `/set_max_price 1 18.5` updates the maximum EUR/TB threshold for alert `1`; use `none` to disable it.
- `/set_max_price 1 0.08 gb` sets an SSD-style maximum EUR/GB threshold; it is converted to EUR/TB internally.
- `/set_capacity 1 16 24` updates the minimum and maximum TB range; use `none` for an open bound.
- `/test` runs a dry scan and reports what would match.
- `/status` shows source and database status.

Alerts are owned per Telegram user. Every authorized user can create, list, pause, resume, update, and delete their own alerts, but cannot manage alerts owned by another authorized user.

Admin commands, restricted to `TELEGRAM_ADMIN_USER_IDS`:

- `/users` lists allowed and disabled users.
- `/allow 123456789 User` adds or re-enables a user with a custom label.
- `/revoke 123456789` disables a user.

Admin users get an expanded Telegram command menu for these admin commands.
They also get admin tiles in the inline navigation.

Accepted alert keys:

- `name`: alert name.
- `min_tb`, `max_tb`: capacity range in TB.
- `max_eur_tb`: maximum EUR/TB; this can notify immediately, before 30 days of history exist.
- `max_eur_gb`: maximum EUR/GB for SSD alerts; converted to EUR/TB internally.
- `condition`: `new`, `used`, or comma-separated values.
- `media`: `rotational`, `solid_state`, or comma-separated values.
- `category`: DiskPrices-style category filter. Supported values: `external_3_5`, `external_2_5`, `internal_3_5`, `internal_2_5`, `internal_hybrid`, `internal_sas`, `external_ssd`, `internal_ssd`, `m2_sata`, `m2_nvme`, `u2_u3`.
- `interface`: connection filter. Supported values: `sata`, `sas`, `nvme`, `usb`.
- Sources are configured on the backend through environment variables and scanner connectors, not through the Telegram alert editor.
- `discount`: minimum discount percentage compared with the rolling 30 day median; default `5`.
- `cooldown`: hours before repeating a notification for the same product unless price drops further; default `24`.

## Debian deployment

Example:

```bash
sudo useradd --system --home /opt/diskcount --shell /usr/sbin/nologin diskcount
sudo mkdir -p /opt/diskcount /var/lib/diskcount
sudo chown -R diskcount:diskcount /opt/diskcount /var/lib/diskcount
sudo cp -r . /opt/diskcount
cd /opt/diskcount
sudo -u diskcount python3.11 -m venv .venv
sudo -u diskcount .venv/bin/python -m pip install -e .
sudo cp deploy/diskcount.env.example /etc/diskcount.env
sudo nano /etc/diskcount.env
sudo cp deploy/diskcount.service /etc/systemd/system/diskcount.service
sudo systemctl daemon-reload
sudo systemctl enable --now diskcount
sudo journalctl -u diskcount -f
```

Initialize the database before first start if desired:

```bash
sudo -u diskcount /opt/diskcount/.venv/bin/python -m diskcount init-db
```

## SSH troubleshooting

If the VPS SSH port times out after failed login attempts, unblock the client IP from the provider console, rescue shell, or another already-authorized machine.

Current local public IP observed during setup:

```text
<YOUR_CLIENT_IP>
```

Useful Debian commands:

```bash
sudo fail2ban-client status
sudo fail2ban-client status sshd
sudo fail2ban-client set sshd unbanip <YOUR_CLIENT_IP>
sudo systemctl restart ssh
```

If fail2ban is not installed or does not show the ban, check the firewall:

```bash
sudo ufw status numbered
sudo nft list ruleset | grep -n "<YOUR_CLIENT_IP>\|<SSH_PORT>\|ssh"
sudo iptables -S | grep "<YOUR_CLIENT_IP>\|<SSH_PORT>\|ssh"
```

If the VPS provider console shows `[UFW BLOCK]` messages over the login prompt, they are kernel firewall logs printed on the TTY. Press `Enter`, type the Linux username and password normally, then silence console kernel logs for the current session:

```bash
sudo dmesg -n 1
```

If the server was configured for SSH key-only access and the `debian` account has no password, the normal provider TTY console cannot log in as `debian`. Use the provider rescue mode, a root console offered by the provider, or a firewall panel rule instead. In rescue mode, mount the installed system disk, then inspect and edit the installed system files from the mounted path:

```bash
lsblk
sudo mkdir -p /mnt/diskcount-root
sudo mount /dev/sdX1 /mnt/diskcount-root
sudo chroot /mnt/diskcount-root
```

After entering the installed system through `chroot`, run the unban/firewall commands below, then exit and reboot normally.

Server specifics from the server audit:

- working user: `debian`
- working key: `~/.ssh/deployment_key`
- SSH port: `<SSH_PORT>`
- `PasswordAuthentication no`
- `KbdInteractiveAuthentication no`
- `PermitRootLogin no`
- `MaxAuthTries 3`
- fail2ban jail: `sshd`
- fail2ban `bantime = 3600`, `findtime = 600`, `maxretry = 3`

After three bad key/user attempts, wait up to one hour or unban the IP from rescue mode.

Current fail2ban SSH whitelist on server:

```text
127.0.0.1/8
::1
<YOUR_CLIENT_IP>
```

Only the local trusted client IP is explicitly allowed in UFW for `<SSH_PORT>/tcp`; fail2ban remains active for all other remote IPs.

Server and Server SSH whitelist:

- `<SERVER_IP>` / `server`: only `<YOUR_CLIENT_IP>` is in fail2ban `ignoreip` and explicitly allowed in UFW for `<SSH_PORT>/tcp`.
- `<SERVER_IP>` / `server`: only `<YOUR_CLIENT_IP>` is in fail2ban `ignoreip` and explicitly allowed in UFW for `<SSH_PORT>/tcp`.

To explicitly allow this workstation on the custom SSH port:

```bash
sudo ufw allow from <YOUR_CLIENT_IP> to any port <SSH_PORT> proto tcp
sudo ufw reload
```

Expected working SSH command from this workstation:

```powershell
ssh -i $env:USERPROFILE\.ssh\deployment_key -p <SSH_PORT> debian@<SERVER_IP>
```

## CLI

- `diskcount check`: dry-run scan, equivalent to checking current sources without persisting observations.
- `diskcount check --persist`: check and persist observations.
- `diskcount scan --dry-run`: explicit dry-run scan.
- `diskcount list --min-tb 16 --max-eur-tb 20 --media rotational`: print the best current offers sorted by EUR/TB.
- `diskcount run`: start Telegram polling and the background scheduler.

Current production note: server was updated on 2026-04-19 to commit `028186a`; the service is active, scans every
4 hours, and the first deployed scan fetched 1049 offers with 0 source errors. If Telegram logs intermittent
`TelegramConflictError`, verify no second bot poller is running before changing the token.
