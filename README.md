# immich-guardian

A Go service that watches your self-hosted Immich installation and keeps it healthy.

---

## Why this exists

I was paying for Google Photos and iCloud to store my personal photos and videos. Both services compress your media, have monthly subscription costs, and ultimately store your memories on someone else's servers — with no real guarantee of permanence or privacy.

The alternative was to self-host [Immich](https://immich.app/) — an open source Google Photos replacement — on a spare Mac at home, with photos stored on an external Samsung T7 SSD. This worked well, but introduced a new set of problems:

- **What if the SSD gets unplugged?** Photos stop syncing silently and you don't know until it's too late.
- **What if the Docker container crashes?** Same problem — your iPhone thinks it's backing up but nothing is happening.
- **What about the Postgres database?** Immich stores all metadata (albums, face recognition, timestamps) in Postgres. The photos themselves are safe on the SSD, but losing the database means losing all organization. It needs regular backups.
- **Videos eat disproportionate space.** iPhone videos in HEVC are large. 304 videos accounted for 8GB out of an 11GB library. They needed compression, but without wasting SSD writes by creating unnecessary temp files during encoding.

`immich-guardian` solves all of this in a single Go binary — no dependencies, no cron jobs to maintain, no scripts scattered across the system.

---

## What it does

### 🔍 Monitor (every 2 minutes)
- Detects if your SSD gets unplugged → emergency Pushover notification
- Detects if `immich_server` or `immich_postgres` crashes → emergency notification
- Warns when SSD hits 75% full, alerts at 90%

### 🗄 Backup (every Sunday 3am)
- Runs `pg_dumpall` from inside the Postgres container
- Saves dump to your SSD at `BACKUP_PATH/dump_YYYY-MM-DD.sql`
- Auto-rotates, keeping last 4 weekly backups
- Sends Pushover notification on success or failure

### 🎬 Video Compression (every Sunday 4am)
- Finds uncompressed `.mov`/`.mp4`/`.m4v` files in your Immich library
- Compresses using H.265 (libx265) via ffmpeg — same codec iPhone uses, better compression
- ffmpeg writes to a `.guardian_tmp` file, then atomically renames over the original
- If compression fails at any point, the original file is completely untouched
- Skips files modified in the last 10 minutes (might still be uploading)
- Skips if compression saves less than 10% (already well-compressed)
- Tracks processed files in `.guardian_processed.json` to avoid re-processing
- Sends a summary notification with space saved

### 📊 Reports
- **Weekly**: photo/video counts, SSD usage, container status
- **Monthly**: total media files, backup count, overall health

---

## Setup

### Prerequisites

```bash
brew install ffmpeg   # for video compression
```

### 1. Clone

```bash
git clone <repo>
cd immich-guardian
```

### 2. Configure

```bash
cp .env.example .env
# edit .env with your values
```

Get Pushover credentials at [pushover.net](https://pushover.net):
- Create an Application → get **App Token** → `PUSHOVER_APP_KEY`
- Your account **User Key** → `PUSHOVER_USER_KEY`

### 3. Start

```bash
./start.sh
```

Validates your environment, builds the binary fresh, and launches guardian as a background process. Logs are saved to `guardian.log` in the same directory.

### 4. Stop

```bash
./stop.sh
```

Sends a graceful `SIGTERM` and waits up to 10 seconds for a clean exit before force-killing.

### 5. Restart

```bash
./stop.sh && ./start.sh
```

### Watching logs

```bash
tail -f guardian.log
```

---

## SSD Write Strategy

The video compressor is built around minimizing SSD writes. MP4 requires seekable output — ffmpeg cannot stream compressed video to stdout because it needs to seek back and rewrite the file header after encoding completes. Piping to stdout simply doesn't work with the MP4 container format.

The actual flow:

```
read original from SSD → ffmpeg encodes → write .guardian_tmp → atomic rename over original
```

Write count per file: **1 write + 1 rename**. The rename is a single syscall — there is no window where the file is missing or partially written. If ffmpeg fails at any point, the `.guardian_tmp` is deleted and the original is never touched.

---

## Migrating to ThinkPad/Linux

The plan is to eventually move this to a dedicated ThinkPad running Ubuntu/Omarchy for better Docker performance (no macOS virtualization overhead). Migration steps:

1. Copy the repo to the ThinkPad
2. Update paths in `.env` (SSD will be `/media/muaaz/L1/...` on Linux)
3. Run `./start.sh` — same as before
4. Optionally register as a systemd service for auto-start on boot