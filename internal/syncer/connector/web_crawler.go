//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package connector

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"go.uber.org/zap"
	"golang.org/x/net/html"

	"ragflow/internal/common"
	"ragflow/internal/utility"
)

const (
	defaultWebCrawlerBatchSize = 10
	defaultWebCrawlerUserAgent = "RAGFlow-WebCrawler/1.0"
	defaultWebCrawlerMaxDepth  = 2
	maxWebCrawlerDepth         = 5
	defaultWebCrawlerMaxPages  = 300
	maxWebCrawlerPagesLimit    = 2000
	maxWebCrawlerDelayMs       = 5000

	WebCrawlModeAuto          = "auto"
	WebCrawlModeHTMLLinks     = "html_links"
	WebCrawlModeSSRSPA        = "ssr_spa"
	WebCrawlModeSitemapHybrid = "sitemap_hybrid"

	WebExtractStrategyArticleClean = "article_clean"
	WebExtractStrategyFullPage     = "full_page"
)

// webCrawlerAssertURLSafe is the SSRF guard, indirected so unit tests can stub DNS resolution.
var webCrawlerAssertURLSafe = common.AssertURLSafe

var validSlugRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_-]{0,240}$`)

var skippedAssetExtensions = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".map": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".svg": true, ".ico": true, ".bmp": true, ".avif": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".otf": true,
	".mp3": true, ".mp4": true, ".wav": true, ".ogg": true, ".webm": true,
	".zip": true, ".tar": true, ".gz": true, ".rar": true, ".7z": true,
	".exe": true, ".dmg": true, ".apk": true, ".iso": true,
	".xml": true, ".json": true, ".rss": true, ".atom": true,
}

// WebCrawlerConnector recursively crawls a website starting from start_url.
//
// It supports multiple crawling mechanisms tailored to different website architectures:
//   - "auto": Combines sitemap.xml discovery (if available), recursive HTML <a href> crawling,
//     and SSR/SPA Hydration state extraction (__NUXT_DATA__, __NEXT_DATA__, and Nuxt CMS API pagination).
//   - "html_links": Breadth-first traversal over standard <a href> links.
//   - "ssr_spa": HTML <a href> links plus SSR/SPA hydration JSON route discovery (ideal for Nuxt 3 / Next.js).
//   - "sitemap_hybrid": Seeds from sitemap.xml and then crawls child links recursively up to max_depth.
type WebCrawlerConnector struct {
	startURL            string
	crawlMode           string
	extractStrategy     string
	maxDepth            int
	maxPages            int
	sameDomainOnly      bool
	urlFilter           *regexp.Regexp
	excludeURLFilter    *regexp.Regexp
	followPDFLinks      bool
	restrictPDFToDomain bool
	crawlDelay          time.Duration
	userAgent           string
	batchSize           int

	fetch sitemapFetchFunc
}

type webCrawlerQueueItem struct {
	url       string
	depth     int
	parentURL string
}

// NewWebCrawlerConnector constructs a WebCrawlerConnector from raw connector configuration.
func NewWebCrawlerConnector(config map[string]any) (*WebCrawlerConnector, error) {
	startURL := strings.TrimSpace(firstNonEmpty(
		stringConfig(config["start_url"]),
		stringConfig(config["url"]),
		stringConfig(config["base_url"]),
	))

	crawlMode := strings.ToLower(strings.TrimSpace(stringConfig(config["crawl_mode"])))
	if crawlMode == "" {
		crawlMode = WebCrawlModeAuto
	}
	switch crawlMode {
	case WebCrawlModeAuto, WebCrawlModeHTMLLinks, WebCrawlModeSSRSPA, WebCrawlModeSitemapHybrid:
	default:
		return nil, fmt.Errorf("unsupported crawl_mode %q (expected auto, html_links, ssr_spa, or sitemap_hybrid)", crawlMode)
	}

	extractStrategy := strings.ToLower(strings.TrimSpace(stringConfig(config["extract_strategy"])))
	if extractStrategy == "" {
		extractStrategy = WebExtractStrategyArticleClean
	}
	switch extractStrategy {
	case WebExtractStrategyArticleClean, WebExtractStrategyFullPage:
	default:
		return nil, fmt.Errorf("unsupported extract_strategy %q (expected article_clean or full_page)", extractStrategy)
	}

	maxDepth := configInt(config["max_depth"], defaultWebCrawlerMaxDepth)
	if maxDepth < 0 {
		maxDepth = 0
	}
	if maxDepth > maxWebCrawlerDepth {
		maxDepth = maxWebCrawlerDepth
	}

	maxPages := configInt(config["max_pages"], defaultWebCrawlerMaxPages)
	if maxPages <= 0 {
		maxPages = defaultWebCrawlerMaxPages
	}
	if maxPages > maxWebCrawlerPagesLimit {
		maxPages = maxWebCrawlerPagesLimit
	}

	delayMs := configInt(config["crawl_delay_ms"], 0)
	if delayMs < 0 {
		delayMs = 0
	}
	if delayMs > maxWebCrawlerDelayMs {
		delayMs = maxWebCrawlerDelayMs
	}

	userAgent := strings.TrimSpace(stringConfig(config["user_agent"]))
	if userAgent == "" {
		userAgent = defaultWebCrawlerUserAgent
	}

	var urlFilter *regexp.Regexp
	if pattern := strings.TrimSpace(stringConfig(config["url_filter"])); pattern != "" {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("url_filter is not a valid regex: %w", err)
		}
		urlFilter = compiled
	}

	var excludeURLFilter *regexp.Regexp
	if pattern := strings.TrimSpace(stringConfig(config["exclude_url_filter"])); pattern != "" {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("exclude_url_filter is not a valid regex: %w", err)
		}
		excludeURLFilter = compiled
	}

	c := &WebCrawlerConnector{
		startURL:            startURL,
		crawlMode:           crawlMode,
		extractStrategy:     extractStrategy,
		maxDepth:            maxDepth,
		maxPages:            maxPages,
		sameDomainOnly:      configBoolDefault(config["same_domain_only"], true),
		urlFilter:           urlFilter,
		excludeURLFilter:    excludeURLFilter,
		followPDFLinks:      configBoolDefault(config["follow_pdf_links"], false),
		restrictPDFToDomain: configBoolDefault(config["restrict_pdf_to_domain"], true),
		crawlDelay:          time.Duration(delayMs) * time.Millisecond,
		userAgent:           userAgent,
		batchSize:           configInt(config["batch_size"], defaultWebCrawlerBatchSize),
	}
	c.fetch = c.fetchRemote
	return c, nil
}

// Validate validates connector settings and verifies the start URL is reachable.
func (c *WebCrawlerConnector) Validate(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("web_crawler connector is nil")
	}
	if c.startURL == "" {
		return fmt.Errorf("start_url is required")
	}
	if err := c.validateURL(c.startURL); err != nil {
		return err
	}
	if c.batchSize <= 0 {
		return fmt.Errorf("batch_size must be a positive integer")
	}
	body, _, err := c.fetch(ctx, c.startURL)
	if err != nil {
		return fmt.Errorf("failed to fetch start_url %s: %w", c.startURL, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("start_url %s returned empty content", c.startURL)
	}
	return nil
}

// ValidateConnectorSetting validates settings from an unsaved configuration.
func (c *WebCrawlerConnector) ValidateConnectorSetting(ctx context.Context, _ map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, connectorSettingValidationTimeout)
	defer cancel()
	return c.Validate(ctx)
}

// OpenSync opens a recursive web crawling session.
func (c *WebCrawlerConnector) OpenSync(ctx context.Context, request SyncRequest) (SyncSession, error) {
	if err := c.validateURL(c.startURL); err != nil {
		return nil, err
	}
	canonicalStart := normalizeCrawlURL(c.startURL)
	if canonicalStart == "" {
		return nil, fmt.Errorf("invalid start_url %q", c.startURL)
	}

	session := &webCrawlerSyncSession{
		connector:   c,
		queue:       make([]webCrawlerQueueItem, 0, 64),
		visited:     make(map[string]struct{}, 128),
		seenPDFs:    make(map[string]struct{}),
		originHost:  normalizeHost(c.startURL),
		baseOrigin:  originOf(c.startURL),
		skipUntilID: "",
	}

	if request.Resume != nil {
		anchor := firstNonEmpty(request.Resume.SourceID, request.Resume.Cursor)
		if anchor == "" {
			return nil, fmt.Errorf("web_crawler sync checkpoint has no source anchor: %w", ErrSyncResumeInvalid)
		}
		session.skipUntilID = anchor
	}

	// Seed from sitemap.xml first when crawl_mode is auto or sitemap_hybrid.
	if c.crawlMode == WebCrawlModeAuto || c.crawlMode == WebCrawlModeSitemapHybrid {
		session.seedFromSitemap(ctx)
	}

	// Always ensure start_url itself is queued at depth 0.
	session.enqueue(canonicalStart, 0, "")

	return session, nil
}

// OpenPrune executes a discovery pass to build the current live document snapshot.
func (c *WebCrawlerConnector) OpenPrune(ctx context.Context, request PruneRequest) (PruneSession, error) {
	syncSession, err := c.OpenSync(ctx, SyncRequest{FromBeginning: true})
	if err != nil {
		return nil, err
	}
	defer syncSession.Close()

	var documents []SlimDocument
	for {
		batch, err := syncSession.NextBatch(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		for _, doc := range batch.Documents {
			documents = append(documents, SlimDocument{SourceID: doc.SourceID})
		}
	}
	return &sitemapPruneSession{documents: documents, batchSize: c.batchSize}, nil
}

func (c *WebCrawlerConnector) validateURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("URL must be a valid http or https URL: %q", rawURL)
	}
	if _, _, err := webCrawlerAssertURLSafe(rawURL); err != nil {
		return err
	}
	return nil
}

func (c *WebCrawlerConnector) urlAllowed(rawURL, originHost string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	if c.sameDomainOnly && normalizeHost(rawURL) != originHost {
		return false
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	if skippedAssetExtensions[ext] {
		return false
	}
	if c.excludeURLFilter != nil && c.excludeURLFilter.MatchString(rawURL) {
		return false
	}
	if c.urlFilter != nil && !c.urlFilter.MatchString(rawURL) {
		return false
	}
	return true
}

func (c *WebCrawlerConnector) fetchRemote(ctx context.Context, rawURL string) ([]byte, string, error) {
	currentURL := rawURL
	for redirects := 0; redirects <= maxSitemapRedirects; redirects++ {
		hostname, resolvedIP, err := webCrawlerAssertURLSafe(currentURL)
		if err != nil {
			return nil, "", err
		}
		client := utility.PinnedHTTPClient(hostname, resolvedIP, sitemapFetchTimeout)
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,application/xml;q=0.9,*/*;q=0.8")
		resp, err := client.Do(req) // #nosec G107
		if err != nil {
			return nil, "", fmt.Errorf("failed to fetch URL: %w", err)
		}

		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			location := resp.Header.Get("Location")
			resp.Body.Close()
			if location == "" {
				return nil, "", fmt.Errorf("redirect response missing Location header")
			}
			base, parseErr := url.Parse(currentURL)
			if parseErr != nil {
				return nil, "", parseErr
			}
			next, resolveErr := base.Parse(location)
			if resolveErr != nil {
				return nil, "", resolveErr
			}
			currentURL = next.String()
			continue
		}

		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return nil, "", fmt.Errorf("remote URL returned HTTP %d", resp.StatusCode)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxSitemapFileSize+1))
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		resp.Body.Close()
		if readErr != nil {
			return nil, "", fmt.Errorf("failed to read remote content: %w", readErr)
		}
		if int64(len(data)) > maxSitemapFileSize {
			return nil, "", fmt.Errorf("remote file exceeds the maximum allowed size of %d bytes", maxSitemapFileSize)
		}
		return data, contentType, nil
	}
	return nil, "", fmt.Errorf("exceeded %d redirects fetching %s", maxSitemapRedirects, rawURL)
}

type webCrawlerSyncSession struct {
	connector   *WebCrawlerConnector
	queue       []webCrawlerQueueItem
	qIndex      int
	visited     map[string]struct{}
	pagesOutput int

	pendingPDFs []sitemapPDFLink
	pdfIndex    int
	seenPDFs    map[string]struct{}

	originHost      string
	baseOrigin      string
	nuxtAPIChecked  bool
	skipUntilID     string
	resumeSatisfied bool
}

func (s *webCrawlerSyncSession) enqueue(rawURL string, depth int, parentURL string) {
	canonical := normalizeCrawlURL(rawURL)
	if canonical == "" {
		return
	}
	if _, seen := s.visited[canonical]; seen {
		return
	}
	if len(s.visited) >= s.connector.maxPages*3 {
		return
	}
	s.visited[canonical] = struct{}{}
	s.queue = append(s.queue, webCrawlerQueueItem{
		url:       canonical,
		depth:     depth,
		parentURL: parentURL,
	})
}

func (s *webCrawlerSyncSession) seedFromSitemap(ctx context.Context) {
	sitemapURL := strings.TrimRight(s.baseOrigin, "/") + "/sitemap.xml"
	if strings.HasSuffix(strings.ToLower(s.connector.startURL), ".xml") {
		sitemapURL = s.connector.startURL
	}
	if err := s.connector.validateURL(sitemapURL); err != nil {
		return
	}
	data, _, err := s.connector.fetch(ctx, sitemapURL)
	if err != nil {
		return
	}
	doc, err := parseSitemapXML(data)
	if err != nil || doc == nil {
		return
	}
	for _, entry := range doc.entries {
		if s.connector.urlAllowed(entry.loc, s.originHost) {
			s.enqueue(entry.loc, 0, sitemapURL)
		}
	}
}

func (s *webCrawlerSyncSession) NextBatch(ctx context.Context) (SyncBatch, error) {
	batchSize := s.connector.batchSize
	documents := make([]SourceDocument, 0, batchSize)

	for s.qIndex < len(s.queue) && len(documents) < batchSize && s.pagesOutput < s.connector.maxPages {
		if err := ctx.Err(); err != nil {
			return SyncBatch{}, err
		}
		item := s.queue[s.qIndex]
		s.qIndex++

		if !s.connector.urlAllowed(item.url, s.originHost) && item.depth > 0 {
			continue
		}
		if err := s.connector.validateURL(item.url); err != nil {
			common.Warn("web_crawler: skipping unsafe or invalid URL", zap.String("url", item.url), zap.Error(err))
			continue
		}

		if s.connector.crawlDelay > 0 && s.qIndex > 1 {
			select {
			case <-ctx.Done():
				return SyncBatch{}, ctx.Err()
			case <-time.After(s.connector.crawlDelay):
			}
		}

		body, contentType, err := s.connector.fetch(ctx, item.url)
		if err != nil {
			common.Warn("web_crawler: failed to fetch page", zap.String("url", item.url), zap.Error(err))
			continue
		}

		doc, childLinks, pdfLinks := s.processPage(ctx, item, body, contentType)

		if item.depth < s.connector.maxDepth {
			for _, childURL := range childLinks {
				if s.connector.urlAllowed(childURL, s.originHost) {
					s.enqueue(childURL, item.depth+1, item.url)
				}
			}
		}

		for _, pdfURL := range pdfLinks {
			if _, exists := s.seenPDFs[pdfURL]; exists {
				continue
			}
			s.seenPDFs[pdfURL] = struct{}{}
			s.pendingPDFs = append(s.pendingPDFs, sitemapPDFLink{url: pdfURL, parentURL: item.url})
		}

		if doc == nil {
			continue
		}
		s.pagesOutput++

		if s.skipUntilID != "" && !s.resumeSatisfied {
			if doc.SourceID == s.skipUntilID {
				s.resumeSatisfied = true
			}
			continue
		}
		documents = append(documents, *doc)
	}

	if len(documents) == 0 && s.pagesOutput < s.connector.maxPages {
		for s.pdfIndex < len(s.pendingPDFs) && len(documents) < batchSize && s.pagesOutput < s.connector.maxPages {
			if err := ctx.Err(); err != nil {
				return SyncBatch{}, err
			}
			link := s.pendingPDFs[s.pdfIndex]
			s.pdfIndex++
			if err := s.connector.validateURL(link.url); err != nil {
				continue
			}
			body, contentType, err := s.connector.fetch(ctx, link.url)
			if err != nil || !isPDFResponse(contentType, body) || len(body) == 0 {
				continue
			}
			now := time.Now().UTC()
			doc := SourceDocument{
				SourceID:           webCrawlerSourceID(link.url),
				SemanticIdentifier: sitemapSemanticIdentifier(link.url),
				Extension:          ".pdf",
				Blob:               body,
				UpdatedAt:          now,
				SizeBytes:          int64(len(body)),
				Metadata: map[string]any{
					"start_url":  s.connector.startURL,
					"url":        link.url,
					"web_url":    link.url,
					"parent_url": link.parentURL,
					"crawl_mode": s.connector.crawlMode,
				},
				Fingerprint: contentFingerprint(body),
			}
			s.pagesOutput++
			if s.skipUntilID != "" && !s.resumeSatisfied {
				if doc.SourceID == s.skipUntilID {
					s.resumeSatisfied = true
				}
				continue
			}
			documents = append(documents, doc)
		}
	}

	if s.skipUntilID != "" && !s.resumeSatisfied && s.qIndex >= len(s.queue) && s.pdfIndex >= len(s.pendingPDFs) {
		return SyncBatch{}, fmt.Errorf("web_crawler resume anchor %q was not found: %w", s.skipUntilID, ErrSyncResumeInvalid)
	}

	if len(documents) == 0 {
		return SyncBatch{}, io.EOF
	}
	return SyncBatch{
		Documents:  documents,
		Checkpoint: sitemapSyncCheckpoint(documents[len(documents)-1]),
	}, nil
}

func (s *webCrawlerSyncSession) Close() error {
	return nil
}

func (s *webCrawlerSyncSession) processPage(
	ctx context.Context,
	item webCrawlerQueueItem,
	body []byte,
	contentType string,
) (*SourceDocument, []string, []string) {
	now := time.Now().UTC()

	if isPDFResponse(contentType, body) {
		if len(body) == 0 {
			return nil, nil, nil
		}
		return &SourceDocument{
			SourceID:           webCrawlerSourceID(item.url),
			SemanticIdentifier: sitemapSemanticIdentifier(item.url),
			Extension:          ".pdf",
			Blob:               body,
			UpdatedAt:          now,
			SizeBytes:          int64(len(body)),
			Metadata: map[string]any{
				"start_url":  s.connector.startURL,
				"url":        item.url,
				"web_url":    item.url,
				"parent_url": item.parentURL,
				"depth":      item.depth,
				"crawl_mode": s.connector.crawlMode,
			},
			Fingerprint: contentFingerprint(body),
		}, nil, nil
	}

	// Step 1: Discover outgoing links BEFORE stripping navigation / hydration scripts.
	var childLinks []string
	if item.depth < s.connector.maxDepth {
		childLinks = extractHTMLLinks(body, item.url)
		if s.connector.crawlMode == WebCrawlModeAuto || s.connector.crawlMode == WebCrawlModeSSRSPA {
			hydrationLinks, hasPaginatedPosts := extractHydrationLinks(body, s.baseOrigin)
			childLinks = append(childLinks, hydrationLinks...)
			if hasPaginatedPosts && !s.nuxtAPIChecked {
				s.nuxtAPIChecked = true
				apiLinks := s.discoverNuxtCMSPosts(ctx)
				childLinks = append(childLinks, apiLinks...)
			}
		}
	}

	var pdfLinks []string
	if s.connector.followPDFLinks {
		pdfLinks = extractPDFLinks(body, item.url)
		if s.connector.restrictPDFToDomain {
			filtered := pdfLinks[:0]
			for _, link := range pdfLinks {
				if normalizeHost(link) == s.originHost {
					filtered = append(filtered, link)
				}
			}
			pdfLinks = filtered
		}
	}

	// Step 2: Convert HTML to clean Markdown according to extractStrategy.
	markdownText, pageTitle, err := webCrawlerHTMLToMarkdown(body, s.connector.extractStrategy)
	if err != nil || strings.TrimSpace(markdownText) == "" {
		return nil, childLinks, pdfLinks
	}

	blob := []byte(markdownText)
	metadata := map[string]any{
		"start_url":        s.connector.startURL,
		"url":              item.url,
		"web_url":          item.url,
		"depth":            item.depth,
		"crawl_mode":       s.connector.crawlMode,
		"extract_strategy": s.connector.extractStrategy,
	}
	if item.parentURL != "" {
		metadata["parent_url"] = item.parentURL
	}
	if pageTitle != "" {
		metadata["title"] = pageTitle
	}

	return &SourceDocument{
		SourceID:           webCrawlerSourceID(item.url),
		SemanticIdentifier: sitemapSemanticIdentifier(item.url),
		Extension:          ".md",
		Blob:               blob,
		UpdatedAt:          now,
		SizeBytes:          int64(len(blob)),
		Metadata:           metadata,
		Fingerprint:        contentFingerprint(blob),
	}, childLinks, pdfLinks
}

// discoverNuxtCMSPosts queries /api/posts?page=P&limit=100 when a Nuxt CMS page
// (such as cms.demego.vn) exposes paginated posts in __NUXT_DATA__.
func (s *webCrawlerSyncSession) discoverNuxtCMSPosts(ctx context.Context) []string {
	var discovered []string
	for page := 1; page <= 10; page++ {
		apiURL := fmt.Sprintf("%s/api/posts?page=%d&limit=100", strings.TrimRight(s.baseOrigin, "/"), page)
		if err := s.connector.validateURL(apiURL); err != nil {
			break
		}
		data, _, err := s.connector.fetch(ctx, apiURL)
		if err != nil {
			break
		}
		var payload struct {
			Posts []struct {
				Slug any `json:"slug"`
			} `json:"posts"`
		}
		if err := json.Unmarshal(data, &payload); err != nil || len(payload.Posts) == 0 {
			break
		}
		for _, post := range payload.Posts {
			for _, slug := range extractSlugStrings(post.Slug) {
				if isValidCrawlSlug(slug) {
					discovered = append(discovered, strings.TrimRight(s.baseOrigin, "/")+"/"+strings.TrimLeft(slug, "/"))
				}
			}
		}
		if len(payload.Posts) < 100 {
			break
		}
	}
	return discovered
}

func webCrawlerSourceID(rawURL string) string {
	sum := md5.Sum([]byte(rawURL))
	return "web_crawler:" + hex.EncodeToString(sum[:])
}

func normalizeCrawlURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "" {
		parsed.Path = "/"
	} else if len(parsed.Path) > 1 {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	return parsed.String()
}

func normalizeHost(rawURL string) string {
	h := hostOf(rawURL)
	return strings.TrimPrefix(h, "www.")
}

func originOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

// extractHTMLLinks returns absolute HTTP/HTTPS links discovered in <a href> attributes.
func extractHTMLLinks(body []byte, pageURL string) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	var links []string
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return links
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if !strings.EqualFold(token.Data, "a") {
				continue
			}
			for _, attr := range token.Attr {
				if !strings.EqualFold(attr.Key, "href") {
					continue
				}
				val := strings.TrimSpace(attr.Val)
				if val == "" || strings.HasPrefix(val, "#") ||
					strings.HasPrefix(strings.ToLower(val), "javascript:") ||
					strings.HasPrefix(strings.ToLower(val), "mailto:") ||
					strings.HasPrefix(strings.ToLower(val), "tel:") {
					continue
				}
				resolved, err := base.Parse(val)
				if err != nil || (resolved.Scheme != "http" && resolved.Scheme != "https") {
					continue
				}
				if strings.HasSuffix(strings.ToLower(resolved.Path), ".pdf") {
					continue
				}
				if norm := normalizeCrawlURL(resolved.String()); norm != "" {
					links = append(links, norm)
				}
			}
		}
	}
}

// extractHydrationLinks inspects SSR/SPA hydration payloads (__NUXT_DATA__ and __NEXT_DATA__)
// to discover internal routes/slugs that are not rendered as direct <a href> links.
func extractHydrationLinks(body []byte, baseOrigin string) ([]string, bool) {
	if baseOrigin == "" {
		return nil, false
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, false
	}

	var links []string
	hasPaginatedPosts := false
	origin := strings.TrimRight(baseOrigin, "/")

	// 1. Nuxt 3 Hydration Payload (<script id="__NUXT_DATA__" type="application/json">)
	if nuxtRaw := strings.TrimSpace(doc.Find("script#__NUXT_DATA__").Text()); nuxtRaw != "" {
		var arr []any
		if json.Unmarshal([]byte(nuxtRaw), &arr) == nil && len(arr) > 0 {
			deref := func(v any) any {
				if idxFloat, ok := v.(float64); ok {
					idx := int(idxFloat)
					if idx >= 0 && idx < len(arr) {
						return arr[idx]
					}
				}
				return v
			}

			for _, elem := range arr {
				obj, ok := elem.(map[string]any)
				if !ok {
					continue
				}
				// Detect paginated posts envelope: {"posts": idx, "total": idx, "page": idx, "limit": idx}
				if _, hasPosts := obj["posts"]; hasPosts {
					if _, hasTotal := obj["total"]; hasTotal {
						hasPaginatedPosts = true
					}
				}

				slugRef, hasSlug := obj["slug"]
				if !hasSlug {
					continue
				}
				// Skip pure menu category folders where postId is explicitly null
				if postIDRef, hasPostID := obj["postId"]; hasPostID {
					if deref(postIDRef) == nil {
						continue
					}
				}

				resolvedSlug := deref(slugRef)
				switch val := resolvedSlug.(type) {
				case string:
					if isValidCrawlSlug(val) {
						links = append(links, origin+"/"+strings.TrimLeft(val, "/"))
					}
				case map[string]any:
					// Prefer Vietnamese ("vi") slug first if available, or English ("en") as fallback
					if viRef, ok := val["vi"]; ok {
						if s, ok := deref(viRef).(string); ok && isValidCrawlSlug(s) {
							links = append(links, origin+"/"+strings.TrimLeft(s, "/"))
							continue
						}
					}
					for _, langRef := range val {
						if s, ok := deref(langRef).(string); ok && isValidCrawlSlug(s) {
							links = append(links, origin+"/"+strings.TrimLeft(s, "/"))
						}
					}
				}
			}
		}
	}

	// 2. Next.js Hydration Payload (<script id="__NEXT_DATA__" type="application/json">)
	if nextRaw := strings.TrimSpace(doc.Find("script#__NEXT_DATA__").Text()); nextRaw != "" {
		var root map[string]any
		if json.Unmarshal([]byte(nextRaw), &root) == nil {
			var walkNext func(v any)
			walkNext = func(v any) {
				switch node := v.(type) {
				case map[string]any:
					for k, child := range node {
						if k == "slug" || k == "href" || k == "path" {
							for _, s := range extractSlugStrings(child) {
								if isValidCrawlSlug(s) {
									links = append(links, origin+"/"+strings.TrimLeft(s, "/"))
								}
							}
						}
						walkNext(child)
					}
				case []any:
					for _, child := range node {
						walkNext(child)
					}
				}
			}
			walkNext(root["props"])
		}
	}

	return links, hasPaginatedPosts
}

func extractSlugStrings(v any) []string {
	switch val := v.(type) {
	case string:
		return []string{val}
	case map[string]any:
		if vi, ok := val["vi"].(string); ok && strings.TrimSpace(vi) != "" {
			return []string{vi}
		}
		var out []string
		for _, sub := range val {
			if s, ok := sub.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func isValidCrawlSlug(slug string) bool {
	s := strings.Trim(strings.TrimSpace(slug), "/")
	if s == "" || len(s) > 240 {
		return false
	}
	if strings.HasPrefix(s, "_nuxt") || strings.HasPrefix(s, "_next") || strings.HasPrefix(s, "api/") {
		return false
	}
	return validSlugRegex.MatchString(s)
}

// webCrawlerHTMLToMarkdown converts an HTML page into Markdown according to the selected strategy.
func webCrawlerHTMLToMarkdown(body []byte, strategy string) (string, string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}

	pageTitle := strings.TrimSpace(doc.Find("title").First().Text())

	converter := newMarkdownConverter()
	if strategy == WebExtractStrategyFullPage {
		converter.Remove("script", "style", "noscript", "iframe", "svg")
		out, err := converter.ConvertString(string(body))
		if err != nil {
			return "", pageTitle, err
		}
		return strings.TrimSpace(out), pageTitle, nil
	}

	// Default: WebExtractStrategyArticleClean
	doc.Find("script, style, noscript, nav, header, footer, aside, form, iframe, svg").Remove()

	var targetHTML string
	if article := doc.Find("article").First(); article.Length() > 0 && strings.TrimSpace(article.Text()) != "" {
		targetHTML, _ = goquery.OuterHtml(article)
	} else if mainNode := doc.Find("main").First(); mainNode.Length() > 0 && strings.TrimSpace(mainNode.Text()) != "" {
		targetHTML, _ = goquery.OuterHtml(mainNode)
	} else {
		targetHTML, _ = doc.Html()
	}

	out, err := converter.ConvertString(targetHTML)
	if err != nil {
		return "", pageTitle, err
	}
	cleaned := strings.TrimSpace(out)
	if cleaned != "" && pageTitle != "" && !strings.HasPrefix(cleaned, "# ") {
		cleaned = "# " + pageTitle + "\n\n" + cleaned
	}
	return cleaned, pageTitle, nil
}
