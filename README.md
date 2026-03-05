# websync

Cross-platform Go CLI to crawl a web server directory listing and download matching files (for example, PDF patterns) to your local machine.

## Features

- Recursive crawl of HTML directory listings
- Filter by one or more extensions (for example `.pdf,.svg,.dxf`)
- Preserve remote folder structure in local output folder
- Parallel downloads with configurable worker count
- Optional basic auth support
- Direct file URL mode (`-files`) when you already know exact files
- Bulk direct URL mode via text file (`-file-list`)
- Wildcard matching in `-ext` (for example `rx*.pdf` or `*.pdf`)
- Automatic catalog fallback for `.../catalog/index.php` pages (AJAX pattern listing)
- Optional `-insecure` mode for trusted local/self-signed HTTPS servers
- Browser-like request headers by default (`User-Agent`) for better compatibility
- Dry-run mode and include/exclude path filtering
- `-urls-only` output for easy copy into `-file-list`

## Requirements

- Go 1.22+ installed

## Build

### macOS/Linux

```bash
go mod tidy
go build -o websync
```

### Windows (PowerShell)

```powershell
go mod tidy
go build -o websync.exe
```

## Cross-compile from macOS/Linux

```bash
# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -o websync-mac-arm64

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -o websync-mac-amd64

# Windows x64
GOOS=windows GOARCH=amd64 go build -o websync.exe

# Windows ARM64
GOOS=windows GOARCH=arm64 go build -o websync-arm64.exe
```

## Usage

### Show help

```bash
./websync -h
```

### Check for app updates (GitHub releases)

`websync` can query GitHub releases using `github.com/amarillier/go-update-checker`.

```bash
./websync \
  -check-updates \
  -update-owner "amarillier" \
  -update-repo "KrankyBearWebSync" \
  -update-url "https://github.com/amarillier/KrankyBearWebSync/releases" \
  -url "https://example.com/" \
  -ext ".pdf"
```

Update-check flags:

- `-check-updates`: enable update checks
- `-update-owner`: GitHub owner/user (default: `amarillier`)
- `-update-repo`: GitHub repo name (required when update checking is enabled)
- `-update-url`: optional releases/download URL shown in update message
- `-update-min-days`: minimum days between API checks (default: `1`, use `0` to always check)
- `-update-verbose`: verbose output from the update checker library

Notes:

- Update checks use GitHub's latest release endpoint, so this is most useful after your repo exists and has SemVer-style release tags.
- If update-check flags are invalid, `websync` logs a warning and continues the normal crawl/download flow.

### Basic PDF download (recursive)

```bash
./websync -url "https://example.com/" -ext ".pdf" -out "./downloads" -insecure
```

### Download known direct file URL(s)

```bash
./websync -files "https://www.example.com/rx.pdf" -out "./downloads"
./websync -files "https://example.com/a.pdf,https://example.com/b.pdf" -out "./downloads"
```

Note: `-files` expects file URL(s), not a site root for crawling.

### Download many known files from a list file

Create a text file (one entry per line, `#` comments allowed):

```text
# urls.txt
https://www.steved.com/rx.pdf
https://example.com/docs/free-plan.pdf
/relative/path/from/base.pdf
```

Then run:

```bash
./websync -url "https://example.com/" -file-list "./urls.txt" -out "./downloads"
```

By default, invalid lines are skipped and logged as warnings. To fail fast:

```bash
./websync -url "https://example.com/" -file-list "./urls.txt" -continue-on-list-errors=false
```

### Wildcard matching with -ext

```bash
# Exact extension(s)
./websync -url "https://example.com/" -ext ".pdf,.svg" -dry-run

# Wildcards against file name/path
./websync -url "https://example.com/" -ext "rx*.pdf" -dry-run
./websync -url "https://example.com/" -ext "*.pdf" -dry-run
```

### Catalog index pages (like steved catalog)

If a site does not expose directory listing, but has a catalog page that loads downloadable links via AJAX, `websync` can fall back automatically when URL looks like `.../catalog/index.php`.

```bash
./websync -url "https://www.steved.com/catalog/index.php" -ext "*.pdf" -dry-run
./websync -url "https://www.steved.com/catalog/index.php" -ext "rx*.pdf" -dry-run
```

To create a clean URL list file:

```bash
./websync -url "https://www.steved.com/catalog/index.php" -ext "*.pdf" -dry-run -urls-only > urls.txt
```

Optional tuning flags:

```bash
./websync -url "https://www.steved.com/catalog/index.php" \
  -catalog-result-count 48 \
  -catalog-max-pages 200 \
  -dry-run
```

### Download known relative file path(s) using a base URL

```bash
./websync -url "https://example.com/" -files "/docs/file1.pdf,/docs/file2.pdf" -out "./downloads"
```

### Dry-run first (recommended)

```bash
./websync -url "https://example.com/" -ext ".pdf" -dry-run -insecure
```

### Multiple file types

```bash
./websync -url "https://example.com/" -ext ".pdf,.svg,.dxf" -out "./downloads" -insecure
```

### Restrict to one subpath

```bash
./websync -url "https://example.com/" -ext ".pdf" -include-path "/plans/" -insecure
```

### Exclude an unwanted path

```bash
./websync -url "https://example.com/" -ext ".pdf" -exclude-path "/archive/" -insecure
```

### With basic auth

```bash
./websync -url "https://example.local/" -ext ".pdf" -user "myuser" -pass "mypassword" -insecure
```

### If the server needs referer or custom user-agent

```bash
./websync -files "https://example.com/file.pdf" -referer "https://example.com/"
./websync -files "https://example.com/file.pdf" -user-agent "Mozilla/5.0 ..."
./websync -url "https://example.com/" -file-list "./urls.txt" -referer "https://example.com/"
```

## Windows examples (PowerShell)

```powershell
.\websync.exe -url "https://example.com/" -ext ".pdf" -out ".\downloads" -insecure
.\websync.exe -url "https://example.com/" -ext ".pdf,.svg" -dry-run -insecure
.\websync.exe -files "https://www.steved.com/rx.pdf" -out ".\downloads"
.\websync.exe -url "https://example.com/" -file-list ".\urls.txt" -out ".\downloads"
.\websync.exe -url "https://example.com/" -ext "rx*.pdf" -dry-run
```

## Important notes

- This tool works best when your server exposes directory pages with clickable links (`<a href="...">`).
- If the server does not provide directory listing HTML, crawling will not discover files.
- `-insecure` should only be used for trusted internal/local servers.

## Troubleshooting

- **No files found**
  - Verify the URL points to a directory listing page.
  - If the site blocks listing, use known direct URLs with `-files` or `-file-list`.
  - For catalog-style pages, pass the catalog page URL directly (for example `/catalog/index.php`) so fallback can query AJAX listing.
  - Run with `-dry-run` and check what links are discovered.
  - Confirm extension filter, including leading dot (for example `.pdf`).
- **HTTP 465 (or other anti-bot/proxy errors)**
  - Use direct mode for known files: `-files "https://host/file.pdf"`.
  - Use bulk list mode for batches: `-file-list "./urls.txt"`.
  - Try adding `-referer "https://host/"`.
  - Override `-user-agent` if the site expects a browser-like client.
- **TLS errors**
  - Use `-insecure` for self-signed local HTTPS certificates.
- **Auth errors (401/403)**
  - Provide `-user` and `-pass`.
- **Slow downloads**
  - Increase `-workers` (for example `-workers 20`) if your server can handle more parallel requests.


List all catalog PDFs:
./websync -url "https://www.steved.com/catalog/index.php" -ext "*.pdf" -dry-run -urls-only
List only matches like rx*.pdf:
./websync -url "https://www.steved.com/catalog/index.php" -ext "rx*.pdf" -dry-run -urls-only
Save list to a file for later download:
./websync -url "https://www.steved.com/catalog/index.php" -ext "*.pdf" -dry-run -urls-only > urls.txt
./websync -file-list "./urls.txt" -out "./downloads"