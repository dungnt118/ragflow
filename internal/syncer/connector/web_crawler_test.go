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
	"os"
	"strings"
	"testing"
)

func newWebCrawlerTestConnector(t *testing.T, config map[string]any, fixture sitemapFixture) *WebCrawlerConnector {
	t.Helper()
	restore := webCrawlerAssertURLSafe
	webCrawlerAssertURLSafe = func(rawURL string) (string, string, error) { return "cms.example.com", "203.0.113.10", nil }
	t.Cleanup(func() { webCrawlerAssertURLSafe = restore })

	if _, ok := config["start_url"]; !ok {
		config["start_url"] = "https://cms.example.com/"
	}
	connector, err := NewWebCrawlerConnector(config)
	if err != nil {
		t.Fatalf("NewWebCrawlerConnector failed: %v", err)
	}
	if fixture != nil {
		connector.fetch = fixture.fetch
	}
	return connector
}

func TestWebCrawlerBuiltInRegistration(t *testing.T) {
	restore := webCrawlerAssertURLSafe
	webCrawlerAssertURLSafe = func(rawURL string) (string, string, error) { return "cms.example.com", "203.0.113.10", nil }
	t.Cleanup(func() { webCrawlerAssertURLSafe = restore })

	registry := NewRegistry()
	RegisterBuiltIns(registry)

	instance, err := registry.OpenFromConfig("web_crawler", map[string]any{
		"start_url":  "https://cms.example.com/",
		"crawl_mode": "auto",
	})
	if err != nil {
		t.Fatalf("OpenFromConfig(web_crawler) failed: %v", err)
	}
	if instance == nil {
		t.Fatal("expected non-nil WebCrawlerConnector")
	}
}

func TestWebCrawlerConfigValidation(t *testing.T) {
	emptyConn, err := NewWebCrawlerConnector(map[string]any{})
	if err != nil {
		t.Fatalf("NewWebCrawlerConnector(empty) failed: %v", err)
	}
	if err := emptyConn.Validate(t.Context()); err == nil {
		t.Fatal("expected Validate error for missing start_url")
	}
	if _, err := NewWebCrawlerConnector(map[string]any{
		"start_url":  "https://cms.example.com/",
		"crawl_mode": "unsupported_mode",
	}); err == nil {
		t.Fatal("expected error for unsupported crawl_mode")
	}
	if _, err := NewWebCrawlerConnector(map[string]any{
		"start_url":        "https://cms.example.com/",
		"extract_strategy": "unsupported_strategy",
	}); err == nil {
		t.Fatal("expected error for unsupported extract_strategy")
	}
	if _, err := NewWebCrawlerConnector(map[string]any{
		"start_url":  "https://cms.example.com/",
		"url_filter": "[invalid-regex",
	}); err == nil {
		t.Fatal("expected error for invalid url_filter regex")
	}
}

func TestWebCrawlerHTMLLinksAndCleanArticleExtraction(t *testing.T) {
	fixture := sitemapFixture{
		"https://cms.example.com/": {
			"text/html",
			`<html><head><title>Portal</title></head><body>
				<nav><a href="/doc-1">Doc 1</a><a href="/doc-2">Doc 2</a><a href="/login">Login</a><a href="https://external.org/out">Out</a></nav>
				<main><article><h1>Welcome Portal</h1><p>Main overview text with <a href="/files/spec.pdf">Spec PDF</a>.</p></article></main>
				<footer>Copyright 2026</footer>
			</body></html>`,
		},
		"https://cms.example.com/doc-1": {
			"text/html",
			`<html><head><title>Doc 1 Title</title></head><body>
				<nav><a href="/">Home</a><a href="/doc-2">Sidebar Link</a></nav>
				<article><h1>Doc 1 Heading</h1><p>Detailed guide for module 1. <a href="/doc-deep">Deep Page</a></p></article>
			</body></html>`,
		},
		"https://cms.example.com/doc-2": {
			"text/html",
			`<html><head><title>Doc 2 Title</title></head><body>
				<aside>Sidebar menu noise</aside>
				<article><h1>Doc 2 Heading</h1><p>Detailed guide for module 2.</p></article>
			</body></html>`,
		},
		"https://cms.example.com/login": {
			"text/html",
			`<html><body><main><h1>Login</h1></main></body></html>`,
		},
		"https://cms.example.com/doc-deep": {
			"text/html",
			`<html><body><article><h1>Deep Heading</h1><p>Should not be crawled at depth 1.</p></article></body></html>`,
		},
		"https://cms.example.com/files/spec.pdf": {
			"application/pdf",
			"%PDF-1.7 specification",
		},
	}

	connector := newWebCrawlerTestConnector(t, map[string]any{
		"start_url":          "https://cms.example.com/",
		"crawl_mode":         "html_links",
		"extract_strategy":   "article_clean",
		"max_depth":          1,
		"max_pages":          10,
		"exclude_url_filter": `/login$`,
		"follow_pdf_links":   true,
		"batch_size":         2,
	}, fixture)

	if err := connector.Validate(t.Context()); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	session, err := connector.OpenSync(t.Context(), SyncRequest{FromBeginning: true})
	if err != nil {
		t.Fatalf("OpenSync failed: %v", err)
	}
	batches, documents := drainSitemapSession(t, session)
	if len(batches) == 0 {
		t.Fatal("expected non-empty batches")
	}

	wantURLs := []string{
		"https://cms.example.com/",
		"https://cms.example.com/doc-1",
		"https://cms.example.com/doc-2",
		"https://cms.example.com/files/spec.pdf",
	}
	if got := sitemapDocIDs(documents); strings.Join(got, ",") != strings.Join(wantURLs, ",") {
		t.Fatalf("crawled URLs = %v, want %v", got, wantURLs)
	}

	// Verify article_clean stripped sidebar/nav noise from doc-1 while still discovering links from <nav> on root.
	doc1Body := string(documents[1].Blob)
	if !strings.Contains(doc1Body, "Detailed guide for module 1") {
		t.Fatalf("doc-1 markdown missing main article content: %q", doc1Body)
	}
	if strings.Contains(doc1Body, "Sidebar Link") {
		t.Fatalf("doc-1 markdown should not include <nav> sidebar noise: %q", doc1Body)
	}
}

func TestWebCrawlerNuxtSSRHydrationAndAPIPagination(t *testing.T) {
	// Models https://cms.demego.vn/ where:
	// 1. sitemap.xml only lists the homepage
	// 2. homepage contains <script id="__NUXT_DATA__"> with post slugs and pagination {total: 3, page: 1, limit: 2}
	// 3. /api/posts?page=1&limit=100 returns JSON with the remaining post slugs
	fixture := sitemapFixture{
		"https://cms.example.com/sitemap.xml": {
			"application/xml",
			`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>https://cms.example.com/</loc></url></urlset>`,
		},
		"https://cms.example.com/": {
			"text/html",
			`<!DOCTYPE html><html><head><title>Demego CMS</title></head><body>
				<div id="__nuxt">
					<aside><a href="/gioi-thieu-chung">Giới thiệu chung</a></aside>
					<main><article><h1>Trang chủ hướng dẫn</h1><p>Chọn danh mục bên trái để xem chi tiết.</p></article></main>
				</div>
				<script type="application/json" data-nuxt-data="nuxt-app" id="__NUXT_DATA__">
					[{"posts":1,"total":3,"page":1,"limit":2},{"id":10,"slug":"gioi-thieu-chung"},{"id":11,"slug":"quan-ly-toa-nha"}]
				</script>
			</body></html>`,
		},
		"https://cms.example.com/api/posts?page=1&limit=100": {
			"application/json",
			`{"posts":[{"id":10,"slug":"gioi-thieu-chung"},{"id":11,"slug":"quan-ly-toa-nha"},{"id":12,"slug":"quan-ly-cu-dan"}],"total":3,"page":1,"limit":100}`,
		},
		"https://cms.example.com/gioi-thieu-chung": {
			"text/html",
			`<html><head><title>Giới thiệu chung</title></head><body><aside>Menu 280 links</aside><article><h1>Giới thiệu chung</h1><p>Nội dung giới thiệu phần mềm quản lý tòa nhà.</p></article></body></html>`,
		},
		"https://cms.example.com/quan-ly-toa-nha": {
			"text/html",
			`<html><head><title>Quản lý tòa nhà</title></head><body><aside>Menu 280 links</aside><article><h1>Quản lý tòa nhà</h1><p>Hướng dẫn thiết lập tòa nhà và căn hộ.</p></article></body></html>`,
		},
		"https://cms.example.com/quan-ly-cu-dan": {
			"text/html",
			`<html><head><title>Quản lý cư dân</title></head><body><aside>Menu 280 links</aside><article><h1>Quản lý cư dân</h1><p>Hướng dẫn tiếp nhận và duyệt cư dân.</p></article></body></html>`,
		},
	}

	connector := newWebCrawlerTestConnector(t, map[string]any{
		"start_url":        "https://cms.example.com/",
		"crawl_mode":       "auto",
		"extract_strategy": "article_clean",
		"max_depth":        2,
		"max_pages":        20,
	}, fixture)

	session, err := connector.OpenSync(t.Context(), SyncRequest{FromBeginning: true})
	if err != nil {
		t.Fatalf("OpenSync failed: %v", err)
	}
	_, documents := drainSitemapSession(t, session)

	wantURLs := []string{
		"https://cms.example.com/",
		"https://cms.example.com/gioi-thieu-chung",
		"https://cms.example.com/quan-ly-toa-nha",
		"https://cms.example.com/quan-ly-cu-dan",
	}
	if got := sitemapDocIDs(documents); strings.Join(got, ",") != strings.Join(wantURLs, ",") {
		t.Fatalf("auto Nuxt SSR crawl URLs = %v, want %v", got, wantURLs)
	}

	for _, doc := range documents[1:] {
		text := string(doc.Blob)
		if strings.Contains(text, "Menu 280 links") {
			t.Fatalf("expected <aside> menu to be stripped in article_clean mode, got: %q", text)
		}
	}

	pruneSession, err := connector.OpenPrune(t.Context(), PruneRequest{})
	if err != nil {
		t.Fatalf("OpenPrune failed: %v", err)
	}
	pruneBatch, err := pruneSession.NextBatch(t.Context())
	if err != nil {
		t.Fatalf("prune NextBatch failed: %v", err)
	}
	if len(pruneBatch.Documents) != 4 {
		t.Fatalf("prune documents = %d, want 4", len(pruneBatch.Documents))
	}
}

func TestWebCrawlerLiveDemegoCMS(t *testing.T) {
	if os.Getenv("DEMEGO_LIVE_TEST") != "1" {
		t.Skip("set DEMEGO_LIVE_TEST=1 to run live crawl against https://cms.demego.vn/")
	}
	connector, err := NewWebCrawlerConnector(map[string]any{
		"start_url":        "https://cms.demego.vn/",
		"crawl_mode":       "auto",
		"extract_strategy": "article_clean",
		"max_depth":        1,
		"max_pages":        8,
		"batch_size":       4,
	})
	if err != nil {
		t.Fatalf("NewWebCrawlerConnector failed: %v", err)
	}
	if err := connector.Validate(t.Context()); err != nil {
		t.Fatalf("Validate(cms.demego.vn) failed: %v", err)
	}
	session, err := connector.OpenSync(t.Context(), SyncRequest{FromBeginning: true})
	if err != nil {
		t.Fatalf("OpenSync(cms.demego.vn) failed: %v", err)
	}
	_, documents := drainSitemapSession(t, session)
	if len(documents) < 5 {
		t.Fatalf("expected at least 5 crawled documents from cms.demego.vn, got %d", len(documents))
	}
	for i, doc := range documents {
		t.Logf("Demego Doc [%d]: url=%v title=%v size=%d bytes", i+1, doc.Metadata["url"], doc.Metadata["title"], doc.SizeBytes)
	}
}
