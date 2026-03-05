package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	updatechecker "github.com/amarillier/go-update-checker"
	"golang.org/x/net/html"
)

const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"

const (
	appVersion = "0.1.0" // see FyneApp.toml // (if we ever add a GUI!)
	appID      = "com.github.amarillier.KrankyBearWebSync"
)

var appName = "KrankyBearWebSync"
var appCopyright = "Copyright (c) Allan Marillier, 2026-" + strconv.Itoa(time.Now().Year())

type logger struct {
	json bool
	mu   sync.Mutex
}

type fileMatcher struct {
	exactExts map[string]bool
	globs     []string
}

func (l *logger) event(level, msg string, fields map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if fields == nil {
		fields = map[string]any{}
	}

	if l.json {
		payload := map[string]any{
			"level": level,
			"msg":   msg,
			"time":  time.Now().Format(time.RFC3339),
		}
		for k, v := range fields {
			payload[k] = v
		}
		enc, _ := json.Marshal(payload)
		fmt.Println(string(enc))
		return
	}

	parts := []string{}
	for k, v := range fields {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	if len(parts) > 0 {
		fmt.Printf("[%s] %s (%s)\n", strings.ToUpper(level), msg, strings.Join(parts, ", "))
		return
	}
	fmt.Printf("[%s] %s\n", strings.ToUpper(level), msg)
}

func main() {
	baseURL := flag.String("url", "", "Base URL to crawl, e.g. https://host/root/")
	extFlag := flag.String("ext", ".pdf", "Comma-separated extension(s), e.g. .pdf,.svg")
	filesFlag := flag.String("files", "", "Comma-separated direct file URL(s) or relative path(s), e.g. https://host/a.pdf,/b/c.pdf")
	fileListFlag := flag.String("file-list", "", "Path to text file with one direct file URL/path per line")
	outDir := flag.String("out", "downloads", "Output directory")
	recursive := flag.Bool("recursive", true, "Recursively crawl subdirectories")
	insecure := flag.Bool("insecure", false, "Skip TLS verification (for trusted local servers)")
	user := flag.String("user", "", "Basic auth username (optional)")
	pass := flag.String("pass", "", "Basic auth password (optional)")
	userAgent := flag.String("user-agent", defaultUserAgent, "HTTP User-Agent header")
	referer := flag.String("referer", "", "HTTP Referer header (optional)")
	timeout := flag.Duration("timeout", 20*time.Second, "HTTP timeout")
	workers := flag.Int("workers", max(2, runtime.NumCPU()), "Download worker count")
	dryRun := flag.Bool("dry-run", false, "List files but do not download")
	urlsOnly := flag.Bool("urls-only", false, "When listing results, print only raw URLs")
	skipExisting := flag.Bool("skip-existing", true, "Skip files already present with same size when known")
	continueOnListErrors := flag.Bool("continue-on-list-errors", true, "Skip invalid -file-list lines and continue")
	catalogFallback := flag.Bool("catalog-fallback", true, "If crawl finds nothing and URL looks like a catalog page, try AJAX catalog listing")
	catalogResultCount := flag.Int("catalog-result-count", 48, "Catalog AJAX page size when fallback is used")
	catalogMaxPages := flag.Int("catalog-max-pages", 200, "Catalog AJAX max pages when fallback is used")
	includePath := flag.String("include-path", "", "Only download paths containing this substring")
	excludePath := flag.String("exclude-path", "", "Skip paths containing this substring")
	jsonLogs := flag.Bool("json", false, "Emit JSON logs")
	checkUpdates := flag.Bool("check-updates", false, "Check GitHub releases for a newer app version")
	updateOwner := flag.String("update-owner", "amarillier", "GitHub owner/user for update checks")
	updateRepo := flag.String("update-repo", "", "GitHub repo name for update checks (required with -check-updates)")
	updateURL := flag.String("update-url", "", "Optional releases/download URL shown when an update is found")
	updateMinDays := flag.Int("update-min-days", 1, "Minimum days between GitHub update checks (0 = always)")
	updateVerbose := flag.Bool("update-verbose", false, "Enable verbose output from update checker")
	flag.Parse()

	log := &logger{json: *jsonLogs}
	maybeCheckForUpdates(log, *checkUpdates, *updateOwner, *updateRepo, *updateURL, *updateMinDays, *updateVerbose)

	if strings.TrimSpace(*baseURL) == "" && strings.TrimSpace(*filesFlag) == "" && strings.TrimSpace(*fileListFlag) == "" {
		log.event("error", "provide at least one of -url, -files, or -file-list", nil)
		os.Exit(1)
	}

	matcher := newFileMatcher(*extFlag)
	if len(matcher.exactExts) == 0 && len(matcher.globs) == 0 {
		log.event("error", "no valid extension(s) in -ext", map[string]any{"ext": *extFlag})
		os.Exit(1)
	}

	var root *url.URL
	if strings.TrimSpace(*baseURL) != "" {
		var err error
		root, err = url.Parse(*baseURL)
		if err != nil || root.Scheme == "" || root.Host == "" {
			log.event("error", "invalid URL", map[string]any{"url": *baseURL, "err": err})
			os.Exit(1)
		}
	}

	client := &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: *insecure}, //nolint:gosec
		},
	}

	visitedDirs := map[string]bool{}
	visitedFiles := map[string]bool{}
	files := make([]string, 0, 128)

	if root != nil {
		log.event("info", "starting crawl", map[string]any{"url": root.String(), "recursive": *recursive, "ext": *extFlag})
		if err := crawl(client, root, matcher, *recursive, *user, *pass, *userAgent, *referer, visitedDirs, visitedFiles, &files); err != nil {
			log.event("error", "crawl failed", map[string]any{"err": err})
			os.Exit(1)
		}
		if len(files) == 0 && *catalogFallback && looksLikeCatalogIndex(root) {
			if *catalogResultCount < 1 {
				*catalogResultCount = 48
			}
			if *catalogMaxPages < 1 {
				*catalogMaxPages = 1
			}
			log.event("info", "trying catalog AJAX fallback", map[string]any{"url": root.String(), "result_count": *catalogResultCount, "max_pages": *catalogMaxPages})
			catalogFiles, err := fetchCatalogPatternLinks(client, root, *catalogResultCount, *catalogMaxPages, *user, *pass, *userAgent, *referer)
			if err != nil {
				log.event("warn", "catalog fallback failed", map[string]any{"err": err})
			} else {
				for _, u := range catalogFiles {
					if !visitedFiles[u] {
						visitedFiles[u] = true
						files = append(files, u)
					}
				}
				log.event("info", "catalog fallback found file URL(s)", map[string]any{"count": len(catalogFiles)})
			}
		}
	}

	if strings.TrimSpace(*filesFlag) != "" {
		directFiles, err := parseDirectFileURLs(*filesFlag, root)
		if err != nil {
			log.event("error", "invalid -files input", map[string]any{"err": err})
			os.Exit(1)
		}
		for _, u := range directFiles {
			if !visitedFiles[u] {
				visitedFiles[u] = true
				files = append(files, u)
			}
		}
		log.event("info", "added direct file URL(s)", map[string]any{"count": len(directFiles)})
	}
	if strings.TrimSpace(*fileListFlag) != "" {
		fileListEntries, warnings, err := parseDirectFileList(*fileListFlag, root, *continueOnListErrors)
		if err != nil {
			log.event("error", "invalid -file-list input", map[string]any{"path": *fileListFlag, "err": err})
			os.Exit(1)
		}
		for _, warn := range warnings {
			log.event("warn", "skipping bad file-list entry", map[string]any{"detail": warn, "path": *fileListFlag})
		}
		for _, u := range fileListEntries {
			if !visitedFiles[u] {
				visitedFiles[u] = true
				files = append(files, u)
			}
		}
		log.event("info", "added file URL(s) from list", map[string]any{"count": len(fileListEntries), "path": *fileListFlag})
	}

	downloadList := filterPaths(filterByMatcher(files, matcher), *includePath, *excludePath)
	if len(downloadList) == 0 {
		log.event("info", "no matching files found", map[string]any{"ext": *extFlag})
		return
	}

	log.event("info", "crawl complete", map[string]any{"found": len(downloadList)})
	if *dryRun {
		for _, u := range downloadList {
			if *urlsOnly {
				fmt.Println(u)
				continue
			}
			log.event("info", "dry-run file", map[string]any{"url": u})
		}
		return
	}

	if *workers < 1 {
		*workers = 1
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.event("error", "failed creating output directory", map[string]any{"out": *outDir, "err": err})
		os.Exit(1)
	}

	type result struct {
		url     string
		status  string
		message string
	}

	jobs := make(chan string)
	results := make(chan result, len(downloadList))
	var wg sync.WaitGroup

	workerFn := func() {
		defer wg.Done()
		for fileURL := range jobs {
			if err := downloadFile(client, root, fileURL, *outDir, *user, *pass, *userAgent, *referer, *skipExisting); err != nil {
				results <- result{url: fileURL, status: "failed", message: err.Error()}
				continue
			}
			results <- result{url: fileURL, status: "downloaded"}
		}
	}

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go workerFn()
	}

	for _, fileURL := range downloadList {
		jobs <- fileURL
	}
	close(jobs)

	wg.Wait()
	close(results)

	var ok, skipped, failed int
	for r := range results {
		switch {
		case r.status == "downloaded":
			ok++
			log.event("info", "downloaded", map[string]any{"url": r.url})
		case strings.HasPrefix(r.message, "skipped existing"):
			skipped++
			log.event("info", "skipped", map[string]any{"url": r.url, "reason": r.message})
		default:
			failed++
			log.event("error", "download failed", map[string]any{"url": r.url, "err": r.message})
		}
	}

	log.event("info", "done", map[string]any{"downloaded": ok, "skipped": skipped, "failed": failed})
	if failed > 0 {
		os.Exit(2)
	}
}

func newFileMatcher(raw string) fileMatcher {
	parts := strings.Split(raw, ",")
	m := fileMatcher{
		exactExts: make(map[string]bool, len(parts)),
		globs:     make([]string, 0, len(parts)),
	}
	for _, p := range parts {
		e := strings.ToLower(strings.TrimSpace(p))
		if e == "" {
			continue
		}
		if strings.ContainsAny(e, "*?[") {
			m.globs = append(m.globs, e)
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		m.exactExts[e] = true
	}
	return m
}

func (m fileMatcher) matchesURLPath(urlPath string) bool {
	if urlPath == "" {
		return false
	}

	lowerPath := strings.ToLower(urlPath)
	if m.exactExts[strings.ToLower(path.Ext(lowerPath))] {
		return true
	}

	base := strings.ToLower(path.Base(lowerPath))
	trimmedPath := strings.TrimPrefix(lowerPath, "/")
	for _, pattern := range m.globs {
		if ok, _ := path.Match(pattern, base); ok {
			return true
		}
		if ok, _ := path.Match(pattern, trimmedPath); ok {
			return true
		}
	}
	return false
}

func filterByMatcher(urls []string, matcher fileMatcher) []string {
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if matcher.matchesURLPath(u.EscapedPath()) {
			out = append(out, raw)
		}
	}
	return out
}

func filterPaths(urls []string, includeSubstr, excludeSubstr string) []string {
	includeSubstr = strings.ToLower(strings.TrimSpace(includeSubstr))
	excludeSubstr = strings.ToLower(strings.TrimSpace(excludeSubstr))
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		l := strings.ToLower(u)
		if includeSubstr != "" && !strings.Contains(l, includeSubstr) {
			continue
		}
		if excludeSubstr != "" && strings.Contains(l, excludeSubstr) {
			continue
		}
		out = append(out, u)
	}
	return out
}

func crawl(
	client *http.Client,
	base *url.URL,
	matcher fileMatcher,
	recursive bool,
	user, pass, userAgent, referer string,
	visitedDirs map[string]bool,
	visitedFiles map[string]bool,
	files *[]string,
) error {
	key := base.String()
	if visitedDirs[key] {
		return nil
	}
	visitedDirs[key] = true

	doc, err := fetchHTML(client, base.String(), user, pass, userAgent, referer)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", base.String(), err)
	}

	for _, href := range extractLinks(doc) {
		next, err := base.Parse(href)
		if err != nil || next.Host != base.Host {
			continue
		}
		if matcher.matchesURLPath(next.EscapedPath()) {
			fileKey := next.String()
			if !visitedFiles[fileKey] {
				visitedFiles[fileKey] = true
				*files = append(*files, fileKey)
			}
			continue
		}

		if recursive && isLikelyDirectoryLink(href, next.Path) {
			_ = crawl(client, next, matcher, recursive, user, pass, userAgent, referer, visitedDirs, visitedFiles, files)
		}
	}

	return nil
}

func fetchHTML(client *http.Client, target, user, pass, userAgent, referer string) (*html.Node, error) {
	req, err := buildRequest(http.MethodGet, target, user, pass, userAgent, referer)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return nil, fmt.Errorf("non-HTML response")
	}
	return html.Parse(resp.Body)
}

func extractLinks(doc *html.Node) []string {
	var links []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "a") {
			for _, a := range n.Attr {
				if strings.EqualFold(a.Key, "href") {
					h := strings.TrimSpace(a.Val)
					if h == "" {
						continue
					}
					lh := strings.ToLower(h)
					if strings.HasPrefix(lh, "#") || strings.HasPrefix(lh, "javascript:") {
						continue
					}
					links = append(links, h)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return links
}

func isLikelyDirectoryLink(rawHref, resolvedPath string) bool {
	h := strings.TrimSpace(rawHref)
	if h == "" {
		return false
	}
	if strings.HasSuffix(h, "/") {
		return true
	}
	base := filepath.Base(resolvedPath)
	return !strings.Contains(base, ".")
}

func downloadFile(client *http.Client, root *url.URL, fileURL, outDir, user, pass, userAgent, referer string, skipExisting bool) error {
	_ = root
	u, err := url.Parse(fileURL)
	if err != nil {
		return err
	}

	relPath := strings.TrimPrefix(u.Path, "/")
	if relPath == "" {
		return fmt.Errorf("empty file path")
	}
	targetPath := filepath.Join(outDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	if skipExisting {
		exists, reason, err := shouldSkipDownload(client, fileURL, targetPath, user, pass, userAgent, referer)
		if err == nil && exists {
			return fmt.Errorf("skipped existing (%s)", reason)
		}
	}

	req, err := buildRequest(http.MethodGet, u.String(), user, pass, userAgent, referer)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp := targetPath + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, targetPath)
}

func shouldSkipDownload(client *http.Client, fileURL, targetPath, user, pass, userAgent, referer string) (bool, string, error) {
	stat, err := os.Stat(targetPath)
	if err != nil {
		return false, "", err
	}
	if stat.IsDir() {
		return false, "", fmt.Errorf("target path is a directory")
	}

	req, err := buildRequest(http.MethodHead, fileURL, user, pass, userAgent, referer)
	if err != nil {
		return false, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, "", fmt.Errorf("HEAD returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > 0 && resp.ContentLength == stat.Size() {
		return true, "same size", nil
	}
	return false, "", nil
}

func parseDirectFileURLs(raw string, root *url.URL) ([]string, error) {
	parts := strings.Split(raw, ",")
	out, err := normalizeDirectFileEntries(parts, root)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no usable values")
	}
	return out, nil
}

func parseDirectFileList(filePath string, root *url.URL, continueOnError bool) ([]string, []string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	lines := make([]listEntry, 0, 64)
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, listEntry{lineNo: lineNo, value: line})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	out := make([]string, 0, len(lines))
	warnings := make([]string, 0)
	for _, entry := range lines {
		resolved, err := resolveDirectFileEntry(entry.value, root)
		if err == nil {
			out = append(out, resolved)
			continue
		}
		if continueOnError {
			warnings = append(warnings, fmt.Sprintf("line %d (%s): %v", entry.lineNo, entry.value, err))
			continue
		}
		return nil, nil, fmt.Errorf("line %d: %w", entry.lineNo, err)
	}
	if len(out) == 0 {
		return nil, warnings, fmt.Errorf("no usable values")
	}
	return out, warnings, nil
}

func normalizeDirectFileEntries(items []string, root *url.URL) ([]string, error) {
	out := make([]string, 0, len(items))
	for _, p := range items {
		resolved, err := resolveDirectFileEntry(p, root)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

type listEntry struct {
	lineNo int
	value  string
}

func resolveDirectFileEntry(raw string, root *url.URL) (string, error) {
	item := strings.TrimSpace(raw)
	if item == "" {
		return "", fmt.Errorf("empty value")
	}

	u, err := url.Parse(item)
	if err != nil {
		return "", fmt.Errorf("invalid URL/path %q: %w", item, err)
	}
	if u.Scheme != "" && u.Host != "" {
		return u.String(), nil
	}
	if root == nil {
		return "", fmt.Errorf("relative path %q requires -url", item)
	}
	resolved, err := root.Parse(item)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q against %s: %w", item, root.String(), err)
	}
	return resolved.String(), nil
}

func buildRequest(method, target, user, pass, userAgent, referer string) (*http.Request, error) {
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		return nil, err
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		ua = defaultUserAgent
	}
	req.Header.Set("User-Agent", ua)

	ref := strings.TrimSpace(referer)
	if ref != "" {
		req.Header.Set("Referer", ref)
	}
	return req, nil
}

func looksLikeCatalogIndex(u *url.URL) bool {
	if u == nil {
		return false
	}
	p := strings.ToLower(u.EscapedPath())
	return strings.Contains(p, "/catalog/") && strings.HasSuffix(p, "/index.php")
}

func fetchCatalogPatternLinks(client *http.Client, catalogURL *url.URL, resultCount, maxPages int, user, pass, userAgent, referer string) ([]string, error) {
	ajaxURL, err := catalogURL.Parse("inc/ajax.php")
	if err != nil {
		return nil, err
	}

	collected := make([]string, 0, 256)
	seen := map[string]bool{}
	lastPageAdded := -1

	for pageNum := 0; pageNum < maxPages; pageNum++ {
		pageOffset := pageNum * resultCount
		form := url.Values{}
		form.Set("action", "pattern_get")
		form.Set("category", "")
		form.Set("keyword", "")
		form.Set("catNAME", "All")
		form.Set("page", strconv.Itoa(pageOffset))
		form.Set("resultCount", strconv.Itoa(resultCount))

		req, err := buildRequest(http.MethodPost, ajaxURL.String(), user, pass, userAgent, referer)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		req.Header.Set("Accept", "text/html, */*;q=0.8")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.ContentLength = int64(len(form.Encode()))

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("catalog ajax HTTP %d", resp.StatusCode)
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			break
		}

		doc, err := html.Parse(strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		links := extractLinks(doc)
		addedThisPage := 0
		for _, href := range links {
			next, err := catalogURL.Parse(href)
			if err != nil || next.Host != catalogURL.Host {
				continue
			}
			if !strings.HasSuffix(strings.ToLower(next.EscapedPath()), ".pdf") {
				continue
			}
			u := next.String()
			if seen[u] {
				continue
			}
			seen[u] = true
			collected = append(collected, u)
			addedThisPage++
		}

		if addedThisPage == 0 {
			break
		}
		lastPageAdded = pageNum
	}

	if lastPageAdded == -1 {
		return nil, fmt.Errorf("no links returned by catalog ajax")
	}
	return collected, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maybeCheckForUpdates(log *logger, enabled bool, owner, repo, downloadURL string, minDays int, verbose bool) {
	if !enabled {
		return
	}

	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	downloadURL = strings.TrimSpace(downloadURL)

	if err := validateUpdateCheckConfig(owner, repo, minDays); err != nil {
		log.event("warn", "skipping update check", map[string]any{"err": err.Error()})
		return
	}

	log.event("info", "checking for app updates", map[string]any{"repo": owner + "/" + repo})
	uc := updatechecker.New(owner, repo, appName, downloadURL, minDays, verbose)
	uc.CheckForUpdate(appVersion)

	if uc.UpdateAvailable {
		log.event("warn", "application update available", map[string]any{"repo": owner + "/" + repo})
	} else {
		log.event("info", "application is up to date", map[string]any{"repo": owner + "/" + repo})
	}

	if strings.TrimSpace(uc.Message) != "" {
		fmt.Println(uc.Message)
	}
}

func validateUpdateCheckConfig(owner, repo string, minDays int) error {
	if owner == "" || repo == "" {
		return fmt.Errorf("both -update-owner and -update-repo are required when -check-updates is enabled")
	}
	if minDays < 0 {
		return fmt.Errorf("-update-min-days cannot be negative")
	}
	return nil
}
