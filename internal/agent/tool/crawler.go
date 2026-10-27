//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"golang.org/x/net/html"
)

const crawlerToolName = "web_crawler"

const crawlerToolDescription = "Crawls a web page or website (supporting HTML links and SSR/SPA hydration payloads) and returns its extracted text/markdown content and links."

const (
	maxCrawlerResponseBytes = 4 << 20
	maxCrawlerDepth         = 3
	defaultCrawlerMaxPages  = 10
	maxCrawlerPagesLimit    = 50
)

var crawlerHydrationSlugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9\-_/]{2,160}$`)

// CrawlerConfig holds node-level configuration for CrawlerTool when wired as a
// workflow component or pre-configured Agent tool.
type CrawlerConfig struct {
	Query       string
	ExtractType string
	Proxy       string
	MaxDepth    int
	MaxPages    int
}

// crawlerArgs is the JSON shape the model sends in. query is the
// Python-compatible argument name; url is accepted for older Go callers.
type crawlerArgs struct {
	Query       string `json:"query"`
	URL         string `json:"url,omitempty"`
	MaxDepth    int    `json:"max_depth,omitempty"`
	MaxPages    int    `json:"max_pages,omitempty"`
	ExtractType string `json:"extract_type,omitempty"`
}

func (a crawlerArgs) targetURL(fallback string) string {
	if u := strings.TrimSpace(a.Query); u != "" {
		return u
	}
	if u := strings.TrimSpace(a.URL); u != "" {
		return u
	}
	return strings.TrimSpace(fallback)
}

// crawlerPageResult represents one crawled page when max_depth > 0.
type crawlerPageResult struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Content  string `json:"content,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	Status   int    `json:"status,omitempty"`
}

// crawlerResult is the JSON envelope returned to the model.
type crawlerResult struct {
	URL      string              `json:"url"`
	Title    string              `json:"title,omitempty"`
	Content  string              `json:"content,omitempty"`
	Markdown string              `json:"markdown,omitempty"`
	HTML     string              `json:"html,omitempty"`
	Links    []string            `json:"links,omitempty"`
	Pages    []crawlerPageResult `json:"pages,omitempty"`
	Status   int                 `json:"status,omitempty"`
	Error    string              `json:"_ERROR,omitempty"`
}

// Resolver validates a URL and returns the pinned IP for the host.
type Resolver func(rawURL string) (host string, ip net.IP, err error)

// CrawlerTool fetches a single page (max_depth=0) or crawls same-domain child
// pages up to max_depth (1..3) via HTTPHelper and extracts text, Markdown, and links.
type CrawlerTool struct {
	helper   *HTTPHelper
	resolve  Resolver
	defaults CrawlerConfig
}

var _ ToolComponent = (*CrawlerTool)(nil)

// NewCrawlerTool returns a CrawlerTool using the default HTTPHelper.
func NewCrawlerTool() *CrawlerTool {
	return NewCrawlerToolWith(NewHTTPHelper())
}

// NewCrawlerToolWith returns a CrawlerTool that uses the provided HTTPHelper.
func NewCrawlerToolWith(h *HTTPHelper) *CrawlerTool {
	return NewCrawlerToolWithConfig(h, CrawlerConfig{})
}

// NewCrawlerToolWithConfig returns a CrawlerTool with node-level defaults.
func NewCrawlerToolWithConfig(h *HTTPHelper, cfg CrawlerConfig) *CrawlerTool {
	if h == nil {
		h = NewHTTPHelper()
	}
	return &CrawlerTool{
		helper:   h,
		resolve:  ResolveAndValidate,
		defaults: cfg,
	}
}

// WithResolver replaces the URL resolver with a custom function.
func (c *CrawlerTool) WithResolver(fn Resolver) *CrawlerTool {
	if fn != nil {
		c.resolve = fn
	}
	return c
}

// Info returns the tool's metadata for the chat model.
func (c *CrawlerTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: crawlerToolName,
		Desc: crawlerToolDescription,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The absolute URL to crawl. Must include an http:// or https:// scheme.",
				Required: true,
			},
			"max_depth": {
				Type:     schema.Integer,
				Desc:     "Recursion depth for crawling same-domain links (0 for single page, 1..3 for multi-page spider crawl).",
				Required: false,
			},
			"max_pages": {
				Type:     schema.Integer,
				Desc:     "Maximum number of pages to fetch when max_depth > 0 (default 10, max 50).",
				Required: false,
			},
			"extract_type": {
				Type:     schema.String,
				Desc:     "Extraction format: 'content' (plain text), 'markdown' (clean article Markdown), or 'html'.",
				Required: false,
			},
		}),
	}, nil
}

// ComponentSpec describes Crawler when used as a workflow canvas component.
func (c *CrawlerTool) ComponentSpec() ComponentSpec {
	return ComponentSpec{
		Inputs: map[string]string{
			"query": "Target URL to crawl (http:// or https://).",
		},
		Outputs: map[string]string{
			"content":            "Extracted content from the crawled page(s).",
			"markdown":           "Clean Markdown extracted from the crawled page(s).",
			"html":               "Raw HTML content of the root page.",
			"links":              "Discovered hyperlinks.",
			"formalized_content": "Formatted content for downstream LLM/Agent prompts.",
			"json":               "Structured crawl result payload.",
		},
		InputForm: map[string]any{
			"query": map[string]any{
				"name": "URL",
				"type": "line",
			},
		},
	}
}

// BuildComponentOutputs formats the JSON tool output for downstream canvas nodes.
func (c *CrawlerTool) BuildComponentOutputs(envelope map[string]any) map[string]any {
	content, _ := envelope["content"].(string)
	markdown, _ := envelope["markdown"].(string)
	htmlContent, _ := envelope["html"].(string)
	if markdown == "" {
		markdown = content
	}
	formalized := markdown
	if formalized == "" {
		formalized = content
	}
	return map[string]any{
		"content":            content,
		"markdown":           markdown,
		"html":               htmlContent,
		"links":              envelopeSlice(envelope, "links"),
		"formalized_content": formalized,
		"json":               envelope,
	}
}

// ErrCrawlerDepthUnsupported is returned when the caller asks for a negative
// max_depth or max_depth > maxCrawlerDepth (3).
var ErrCrawlerDepthUnsupported = fmt.Errorf(
	"crawler: max_depth out of supported range (0..%d)",
	maxCrawlerDepth,
)

// InvokableRun fetches a single page (max_depth=0) or crawls same-domain pages
// up to max_depth (1..3) and returns extracted content + links.
func (c *CrawlerTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args crawlerArgs
	if argumentsInJSON == "" {
		return crawlerStubResult(crawlerResult{Error: "arguments are required"}),
			errors.New("crawler: empty arguments")
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return crawlerStubResult(crawlerResult{Error: "invalid JSON: " + err.Error()}),
			fmt.Errorf("crawler: parse arguments: %w", err)
	}

	targetURL := args.targetURL(c.defaults.Query)
	if targetURL == "" {
		return crawlerStubResult(crawlerResult{Error: "query is required"}),
			errors.New("crawler: empty query")
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		return crawlerStubResult(crawlerResult{URL: targetURL, Error: "url must be http or https"}),
			fmt.Errorf("crawler: unsupported url scheme: %s", targetURL)
	}

	maxDepth := args.MaxDepth
	if maxDepth == 0 && c.defaults.MaxDepth > 0 {
		maxDepth = c.defaults.MaxDepth
	}
	if maxDepth < 0 || maxDepth > maxCrawlerDepth {
		return crawlerStubResult(crawlerResult{URL: targetURL, Error: ErrCrawlerDepthUnsupported.Error()}),
			ErrCrawlerDepthUnsupported
	}

	extractType := strings.ToLower(strings.TrimSpace(args.ExtractType))
	if extractType == "" {
		extractType = strings.ToLower(strings.TrimSpace(c.defaults.ExtractType))
	}

	if maxDepth == 0 {
		page, err := c.fetchSinglePage(ctx, targetURL, extractType)
		if err != nil {
			return crawlerStubResult(page), err
		}
		return crawlerJSON(page)
	}

	maxPages := args.MaxPages
	if maxPages <= 0 {
		maxPages = c.defaults.MaxPages
	}
	if maxPages <= 0 {
		maxPages = defaultCrawlerMaxPages
	}
	if maxPages > maxCrawlerPagesLimit {
		maxPages = maxCrawlerPagesLimit
	}

	result, err := c.crawlMultiPage(ctx, targetURL, maxDepth, maxPages, extractType)
	if err != nil {
		return crawlerStubResult(result), err
	}
	return crawlerJSON(result)
}

func (c *CrawlerTool) fetchSinglePage(ctx context.Context, targetURL string, extractType string) (crawlerResult, error) {
	host, pinnedIP, resolveErr := c.resolve(targetURL)
	if resolveErr != nil {
		return crawlerResult{URL: targetURL, Error: resolveErr.Error()}, resolveErr
	}

	resp, err := c.helper.DoPinned(ctx, http.MethodGet, targetURL, "", "", nil, host, pinnedIP)
	if err != nil {
		return crawlerResult{URL: targetURL, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCrawlerResponseBytes+1))
	if err != nil {
		return crawlerResult{URL: targetURL, Status: resp.StatusCode, Error: "read body: " + err.Error()},
			fmt.Errorf("crawler: read body: %w", err)
	}
	if len(body) > maxCrawlerResponseBytes {
		err = errors.New("crawler: response too large")
		return crawlerResult{URL: targetURL, Status: resp.StatusCode, Error: err.Error()}, err
	}

	page, err := extractPageWithFormat(body, targetURL, extractType)
	if err != nil {
		return crawlerResult{URL: targetURL, Status: resp.StatusCode, Error: err.Error()},
			fmt.Errorf("crawler: extract: %w", err)
	}
	page.URL = targetURL
	page.Status = resp.StatusCode
	return page, nil
}

func (c *CrawlerTool) crawlMultiPage(ctx context.Context, startURL string, maxDepth int, maxPages int, extractType string) (crawlerResult, error) {
	rootParsed, err := url.Parse(startURL)
	if err != nil {
		return crawlerResult{URL: startURL, Error: err.Error()}, err
	}
	rootHost := strings.ToLower(rootParsed.Host)

	type queueItem struct {
		rawURL string
		depth  int
	}
	queue := []queueItem{{rawURL: startURL, depth: 0}}
	seen := map[string]struct{}{ normalizeCrawlLink(startURL): {} }

	var (
		rootResult  crawlerResult
		pages       []crawlerPageResult
		allLinks    []string
		seenLinks   = make(map[string]struct{})
		contentBuf  strings.Builder
		markdownBuf strings.Builder
	)

	for idx := 0; idx < len(queue) && len(pages) < maxPages; idx++ {
		item := queue[idx]
		page, rawBody, fetchErr := c.fetchSinglePageWithBody(ctx, item.rawURL, extractType)
		if fetchErr != nil {
			if idx == 0 {
				return page, fetchErr
			}
			continue
		}
		if idx == 0 {
			rootResult = page
		}
		pages = append(pages, crawlerPageResult{
			URL:      page.URL,
			Title:    page.Title,
			Content:  page.Content,
			Markdown: page.Markdown,
			Status:   page.Status,
		})

		if contentBuf.Len() > 0 {
			contentBuf.WriteString("\n\n---\n\n")
		}
		if page.Title != "" {
			contentBuf.WriteString("# " + page.Title + " (" + page.URL + ")\n")
		}
		contentBuf.WriteString(page.Content)

		if markdownBuf.Len() > 0 {
			markdownBuf.WriteString("\n\n---\n\n")
		}
		if page.Markdown != "" {
			markdownBuf.WriteString(page.Markdown)
		} else {
			markdownBuf.WriteString(page.Content)
		}

		for _, link := range page.Links {
			if _, exists := seenLinks[link]; !exists {
				seenLinks[link] = struct{}{}
				allLinks = append(allLinks, link)
			}
		}

		if item.depth < maxDepth {
			discovered := discoverSameDomainLinks(rawBody, item.rawURL, rootHost)
			for _, childURL := range discovered {
				norm := normalizeCrawlLink(childURL)
				if norm == "" {
					continue
				}
				if _, visited := seen[norm]; visited {
					continue
				}
				seen[norm] = struct{}{}
				queue = append(queue, queueItem{rawURL: norm, depth: item.depth + 1})
			}
		}
	}

	rootResult.Content = contentBuf.String()
	rootResult.Markdown = markdownBuf.String()
	rootResult.Links = allLinks
	rootResult.Pages = pages
	return rootResult, nil
}

func (c *CrawlerTool) fetchSinglePageWithBody(ctx context.Context, targetURL string, extractType string) (crawlerResult, []byte, error) {
	host, pinnedIP, resolveErr := c.resolve(targetURL)
	if resolveErr != nil {
		return crawlerResult{URL: targetURL, Error: resolveErr.Error()}, nil, resolveErr
	}

	resp, err := c.helper.DoPinned(ctx, http.MethodGet, targetURL, "", "", nil, host, pinnedIP)
	if err != nil {
		return crawlerResult{URL: targetURL, Error: err.Error()}, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCrawlerResponseBytes+1))
	if err != nil {
		return crawlerResult{URL: targetURL, Status: resp.StatusCode, Error: "read body: " + err.Error()}, nil,
			fmt.Errorf("crawler: read body: %w", err)
	}
	if len(body) > maxCrawlerResponseBytes {
		err = errors.New("crawler: response too large")
		return crawlerResult{URL: targetURL, Status: resp.StatusCode, Error: err.Error()}, nil, err
	}

	page, err := extractPageWithFormat(body, targetURL, extractType)
	if err != nil {
		return crawlerResult{URL: targetURL, Status: resp.StatusCode, Error: err.Error()}, nil,
			fmt.Errorf("crawler: extract: %w", err)
	}
	page.URL = targetURL
	page.Status = resp.StatusCode
	return page, body, nil
}

func extractPageWithFormat(body []byte, pageURL string, extractType string) (crawlerResult, error) {
	page, err := extractPage(body)
	if err != nil {
		return crawlerResult{}, err
	}
	for _, ssrLink := range extractSSRHydrationLinks(body, pageURL) {
		page.Links = appendUniqueString(page.Links, ssrLink)
	}
	markdownText := extractCleanMarkdown(body, page.Title)
	if markdownText == "" {
		markdownText = page.Content
	}
	page.Markdown = markdownText

	switch extractType {
	case "markdown":
		page.Content = markdownText
	case "html":
		page.HTML = string(body)
		page.Content = page.HTML
	}
	return page, nil
}

func extractCleanMarkdown(body []byte, title string) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	doc.Find("script, style, noscript, nav, header, footer, aside, form, iframe, svg").Remove()
	selection := doc.Find("article, main, [role='main'], .article-content, .post-content, .prose, .markdown-body").First()
	targetHTML := ""
	if selection.Length() > 0 && len(strings.TrimSpace(selection.Text())) >= 120 {
		if h, err := goquery.OuterHtml(selection); err == nil {
			targetHTML = h
		}
	}
	if targetHTML == "" {
		if h, err := doc.Html(); err == nil {
			targetHTML = h
		}
	}
	converter := md.NewConverter("", true, nil)
	out, err := converter.ConvertString(targetHTML)
	if err != nil {
		return ""
	}
	out = strings.TrimSpace(out)
	if title != "" && out != "" && !strings.HasPrefix(out, "# ") {
		out = "# " + title + "\n\n" + out
	}
	return out
}

func discoverSameDomainLinks(body []byte, pageURL string, rootHost string) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	var links []string
	seen := make(map[string]struct{})
	addCandidate := func(raw string) {
		resolved, err := base.Parse(strings.TrimSpace(raw))
		if err != nil {
			return
		}
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			return
		}
		if !strings.EqualFold(resolved.Host, rootHost) {
			return
		}
		norm := normalizeCrawlLink(resolved.String())
		if norm == "" {
			return
		}
		if _, exists := seen[norm]; exists {
			return
		}
		seen[norm] = struct{}{}
		links = append(links, norm)
	}

	page, err := extractPage(body)
	if err == nil {
		for _, href := range page.Links {
			addCandidate(href)
		}
	}
	for _, ssrLink := range extractSSRHydrationLinks(body, pageURL) {
		addCandidate(ssrLink)
	}
	return links
}

func extractSSRHydrationLinks(body []byte, pageURL string) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	origin := base.Scheme + "://" + base.Host
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var links []string
	seen := make(map[string]struct{})
	doc.Find("script#__NUXT_DATA__, script#__NEXT_DATA__, script[data-nuxt-data], script[type='application/json']").Each(func(_ int, s *goquery.Selection) {
		rawJSON := strings.TrimSpace(s.Text())
		if rawJSON == "" {
			return
		}
		var payload any
		if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
			return
		}
		var walkJSON func(v any)
		walkJSON = func(v any) {
			switch typed := v.(type) {
			case string:
				candidate := strings.TrimSpace(typed)
				if crawlerHydrationSlugRegex.MatchString(candidate) && strings.Contains(candidate, "-") && !strings.Contains(candidate, "--") {
					full := origin + "/" + strings.TrimPrefix(candidate, "/")
					if _, exists := seen[full]; !exists {
						seen[full] = struct{}{}
						links = append(links, full)
					}
				}
			case []any:
				for _, item := range typed {
					walkJSON(item)
				}
			case map[string]any:
				for _, item := range typed {
					walkJSON(item)
				}
			}
		}
		walkJSON(payload)
	})
	return links
}

func normalizeCrawlLink(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	} else if len(parsed.Path) > 1 {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	return parsed.String()
}

func appendUniqueString(slice []string, item string) []string {
	for _, existing := range slice {
		if existing == item {
			return slice
		}
	}
	return append(slice, item)
}

// extractPage parses the HTML body and returns its title, plain text
// content, and links.
func extractPage(body []byte) (crawlerResult, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return crawlerResult{}, fmt.Errorf("parse html: %w", err)
	}

	var out crawlerResult
	var titleNodes []*html.Node
	var text strings.Builder

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "script", "style", "noscript", "template", "svg":
				// skip non-content subtrees entirely
				return
			case "head":
				// recurse into head only to capture <title>; skip text
				// (so meta tags etc. don't pollute the body text)
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && strings.EqualFold(c.Data, "title") {
						titleNodes = append(titleNodes, c)
					}
				}
				return
			case "title":
				titleNodes = append(titleNodes, n)
			case "a":
				for _, a := range n.Attr {
					if strings.EqualFold(a.Key, "href") {
						href := strings.TrimSpace(a.Val)
						if href != "" && !strings.HasPrefix(href, "#") {
							out.Links = append(out.Links, href)
						}
						break
					}
				}
			}
		}
		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t != "" {
				if text.Len() > 0 {
					text.WriteByte(' ')
				}
				text.WriteString(t)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	// Extract title text (concatenate text nodes inside <title>).
	for _, tn := range titleNodes {
		var t strings.Builder
		for c := tn.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				t.WriteString(c.Data)
			}
		}
		title := strings.TrimSpace(t.String())
		if title != "" {
			out.Title = title
			break
		}
	}

	out.Content = strings.Join(strings.Fields(text.String()), " ")
	return out, nil
}

func crawlerStubResult(r crawlerResult) string {
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf(`{"_ERROR":"crawler: marshal: %s"}`, err)
	}
	return string(b)
}

func crawlerJSON(r crawlerResult) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("crawler: marshal result: %w", err)
	}
	return string(b), nil
}
