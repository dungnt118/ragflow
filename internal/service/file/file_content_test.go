package file

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"ragflow/internal/entity"
	"ragflow/internal/parser/parser"
	"ragflow/internal/storage"
	"ragflow/internal/utility"
)

func TestBytesLooksLikePDF(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"valid header", []byte("%PDF-1.4 content"), true},
		{"too short", []byte("%PD"), false},
		{"plain text", []byte("hello"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := utility.BytesLooksLikePDF(c.data); got != c.want {
			t.Errorf("%s: utility.BytesLooksLikePDF = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLooksLikeHTML(t *testing.T) {
	if !utility.LooksLikeHTML([]byte("<html><body>x</body></html>")) {
		t.Error("should detect <html>")
	}
	if !utility.LooksLikeHTML([]byte("<DIV>x</DIV>")) {
		t.Error("should detect <div> case-insensitively")
	}
	if !utility.LooksLikeHTML([]byte("<body>x</body>")) {
		t.Error("should detect <body>")
	}
	if utility.LooksLikeHTML([]byte("just text")) {
		t.Error("should not detect plain text")
	}
}

func TestParseFileContent_JSONItems(t *testing.T) {
	ctx := t.Context()
	// CSV parser now produces JSON table items. sys.files still needs the
	// readable item text, not the original CSV bytes.
	result := parseFileContent(ctx, "data.csv", []byte("a,b,c\n1,2,3\n"))
	if result == "" {
		t.Fatal("CSV parser returned empty content")
	}
	if strings.Contains(result, "a,b,c\n1,2,3\n") {
		t.Errorf("CSV content used original bytes instead of parsed item text: %q", result)
	}
	if !strings.Contains(result, "<table") || !strings.Contains(result, "1") {
		t.Errorf("CSV content lost readable table text: %q", result)
	}
}

func TestParseResultText_JSONItemsPreservesOrderAndFallsBackToJSON(t *testing.T) {
	result, err := parseResultText(parser.ParseResult{
		JSON: parser.MapsToItems([]map[string]any{
			{"text": "first"},
			{"text": "second"},
			{"value": 3},
		}),
	})
	if err != nil {
		t.Fatalf("parseResultText: %v", err)
	}
	want := "first\nsecond\n{\"value\":3}"
	if result != want {
		t.Fatalf("parseResultText = %q, want %q", result, want)
	}
}

// TestParseResultText_SpreadsheetSegmentsAreTheirOwnMarkup: the wire emits
// self-describing HTML table segments, so the readable preview is exactly
// the item text — no parser-side table re-rendering.
func TestParseResultText_SpreadsheetSegmentsAreTheirOwnMarkup(t *testing.T) {
	segment := "<table><caption>Data</caption>\n<tr><th>a</th><th>b</th></tr>\n<tr><td>1</td><td>2</td></tr>\n</table>\n"
	result, err := parseResultText(parser.ParseResult{
		JSON: parser.MapsToItems([]map[string]any{
			{"text": segment, "doc_type_kwd": "table", "sheet": "Data", "sheet_index": 1},
		}),
	})
	if err != nil {
		t.Fatalf("parseResultText: %v", err)
	}
	if result != segment {
		t.Fatalf("parseResultText = %q, want the segment markup whole", result)
	}
}

func TestParseResultText_EmailJSONKeepsSearchableHeaders(t *testing.T) {
	raw := []byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Parser contract\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nEmail body")
	p := parser.NewEmailParser()
	p.ConfigureFromSetup(map[string]any{"output_format": "json", "fields": []string{"from", "to", "subject", "body"}})
	parsed := p.ParseWithResult(t.Context(), "message.eml", raw)
	if parsed.Err != nil {
		t.Fatalf("ParseWithResult: %v", parsed.Err)
	}
	content, err := parseResultText(parsed)
	if err != nil {
		t.Fatalf("parseResultText: %v", err)
	}
	for _, want := range []string{"from:sender@example.com", "to:recipient@example.com", "subject:Parser contract", "Email body"} {
		if !strings.Contains(content, want) {
			t.Errorf("sys.files text missing %q: %q", want, content)
		}
	}
}

func TestParseResultText_EmptyJSONWithoutFallbackReturnsEmpty(t *testing.T) {
	result, err := parseResultText(parser.ParseResult{})
	if err != nil {
		t.Fatalf("parseResultText: %v", err)
	}
	if result != "" {
		t.Fatalf("parseResultText = %q, want empty text", result)
	}
}

func TestParseAgentUploadContent_JSONUsesParsedItemText(t *testing.T) {
	content, err := parseAgentUploadContent(t.Context(), "data.csv", []byte("a,b\n1,2\n"), "")
	if err != nil {
		t.Fatalf("parseAgentUploadContent: %v", err)
	}
	if strings.Contains(content, "a,b\n1,2\n") {
		t.Fatalf("upload content used original bytes instead of parsed item text: %q", content)
	}
	if !strings.Contains(content, "<table") || !strings.Contains(content, "1") {
		t.Fatalf("upload content lost readable table text: %q", content)
	}
}

func TestParseFileContent_MarkdownOutputFormat(t *testing.T) {
	ctx := t.Context()
	// .md files fall through to default parser or produce text/markdown.
	result := parseFileContent(ctx, "doc.md", []byte("# Title\n\nBody"))
	// This is integration-dependent; verify it doesn't crash and returns something.
	if result == "" {
		t.Skip("markdown parser returned empty (integration-only)")
	}
}

func TestDownloadArchive_FolderAndKnowledgebaseFiles(t *testing.T) {
	db := setupFolderTestDB(t)
	store := storage.NewMemoryStorage()
	factory := storage.GetStorageFactory()
	prev := factory.GetStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(prev) })

	root := &entity.File{ID: "root", ParentID: "root", TenantID: "tenant-1", CreatedBy: "tenant-1", Name: "/", Type: FileTypeFolder}
	kbRoot := &entity.File{ID: "kb-root", ParentID: "root", TenantID: "tenant-1", CreatedBy: "tenant-1", Name: DatasetFolderName, Type: FileTypeFolder, SourceType: FileSourceDataset}
	dsFolder := &entity.File{ID: "ds-folder", ParentID: "kb-root", TenantID: "tenant-1", CreatedBy: "tenant-1", Name: "CMS-QLTN", Type: FileTypeFolder, SourceType: FileSourceDataset}
	loc1 := "doc-loc-1"
	kbFile := &entity.File{ID: "kb-file-1", ParentID: "ds-folder", TenantID: "tenant-1", CreatedBy: "tenant-1", Name: "intro.md", Type: "md", Location: &loc1, SourceType: FileSourceDataset}
	docID := "doc-1"
	docName := "intro.md"
	doc := &entity.Document{ID: docID, KbID: "kb-id-1", Name: &docName, Location: &loc1}
	fileID := kbFile.ID
	f2d := &entity.File2Document{ID: "f2d-1", FileID: &fileID, DocumentID: &docID}

	for _, rec := range []any{root, kbRoot, dsFolder, kbFile, doc, f2d} {
		if err := db.Create(rec).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	if err := store.Put(t.Context(), "kb-id-1", loc1, []byte("# Hello Dataset")); err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	svc := testFileService()
	zipBytes, archiveName, err := svc.DownloadArchive(t.Context(), "tenant-1", []string{"ds-folder"})
	if err != nil {
		t.Fatalf("DownloadArchive failed: %v", err)
	}
	if archiveName != "CMS-QLTN.zip" {
		t.Fatalf("archiveName = %q, want CMS-QLTN.zip", archiveName)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("zip entries = %d, want 1", len(zr.File))
	}
	if zr.File[0].Name != "CMS-QLTN/intro.md" {
		t.Fatalf("entry name = %q, want CMS-QLTN/intro.md", zr.File[0].Name)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("Open zip entry: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll zip entry: %v", err)
	}
	if string(body) != "# Hello Dataset" {
		t.Fatalf("entry body = %q, want %q", string(body), "# Hello Dataset")
	}
}

func TestDownloadArchive_MultiSelectFilesAndEmptyFolder(t *testing.T) {
	db := setupFolderTestDB(t)
	store := storage.NewMemoryStorage()
	factory := storage.GetStorageFactory()
	prev := factory.GetStorage()
	factory.SetStorage(store)
	t.Cleanup(func() { factory.SetStorage(prev) })

	root := &entity.File{ID: "root", ParentID: "root", TenantID: "tenant-1", CreatedBy: "tenant-1", Name: "/", Type: FileTypeFolder}
	emptyDir := &entity.File{ID: "empty-dir", ParentID: "root", TenantID: "tenant-1", CreatedBy: "tenant-1", Name: "EmptyFolder", Type: FileTypeFolder}
	locA := "loc-a"
	fileA := &entity.File{ID: "file-a", ParentID: "root", TenantID: "tenant-1", CreatedBy: "tenant-1", Name: "note.txt", Type: "txt", Location: &locA}

	for _, rec := range []any{root, emptyDir, fileA} {
		if err := db.Create(rec).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	if err := store.Put(t.Context(), "root", locA, []byte("plain text")); err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	svc := testFileService()
	zipBytes, archiveName, err := svc.DownloadArchive(t.Context(), "tenant-1", []string{"empty-dir", "file-a"})
	if err != nil {
		t.Fatalf("DownloadArchive failed: %v", err)
	}
	if archiveName != "files.zip" {
		t.Fatalf("archiveName = %q, want files.zip", archiveName)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	names := make(map[string]bool)
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["EmptyFolder/"] || !names["note.txt"] {
		t.Fatalf("zip entries = %v, want EmptyFolder/ and note.txt", names)
	}
}

