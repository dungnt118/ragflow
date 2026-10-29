package file

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/parser/parser"
	"ragflow/internal/parser/tableutil"
	"ragflow/internal/permission"
	"ragflow/internal/storage"
	"ragflow/internal/utility"
	"strings"
)

// GetFileContent gets file metadata and checks permission for download
// Matches Python's file_api_service.get_file_content function
func (s *FileService) GetFileContent(ctx context.Context, uid, fileID string) (*entity.File, error) {
	file, err := s.fileDAO.GetByID(ctx, dao.DB, fileID)
	if err != nil || file == nil {
		return nil, fmt.Errorf("document not found")
	}
	if err := checkFileAccess(ctx, uid, file, permission.OperationRead); err != nil {
		return nil, err
	}
	return file, nil
}

// ReadFileBlob retrieves raw file bytes from primary storage or via the
// file2document mapping fallback (used by dataset-backed files).
func (s *FileService) ReadFileBlob(ctx context.Context, storageImpl storage.Storage, f *entity.File) ([]byte, error) {
	if storageImpl == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	if f == nil {
		return nil, fmt.Errorf("file is nil")
	}

	var blob []byte
	var getErr error
	if f.Location != nil && *f.Location != "" {
		blob, getErr = storageImpl.Get(ctx, f.ParentID, *f.Location)
	}

	if len(blob) == 0 {
		storageAddr, err := s.GetStorageAddress(ctx, f.ID)
		if err != nil {
			if getErr != nil {
				return nil, fmt.Errorf("failed to get file storage address: %w (primary error: %v)", err, getErr)
			}
			return nil, fmt.Errorf("failed to get file storage address: %w", err)
		}
		blob, getErr = storageImpl.Get(ctx, storageAddr.Bucket, storageAddr.Name)
	}

	if len(blob) == 0 && getErr != nil {
		return nil, getErr
	}
	return blob, nil
}

// DownloadArchive packages one or more files and/or folders (recursively) into
// a single ZIP archive after verifying read permissions on every entry.
func (s *FileService) DownloadArchive(ctx context.Context, uid string, fileIDs []string) ([]byte, string, error) {
	if len(fileIDs) == 0 {
		return nil, "", common.NewCodedError(common.CodeParamError, "ids is required")
	}

	storageImpl := storage.GetStorageFactory().GetStorage()
	if storageImpl == nil {
		return nil, "", fmt.Errorf("storage not initialized")
	}

	seenRootIDs := make(map[string]struct{}, len(fileIDs))
	roots := make([]*entity.File, 0, len(fileIDs))
	for _, rawID := range fileIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, exists := seenRootIDs[id]; exists {
			continue
		}
		seenRootIDs[id] = struct{}{}

		f, err := s.fileDAO.GetByID(ctx, dao.DB, id)
		if err != nil {
			return nil, "", lookupError(err, "document not found")
		}
		if f == nil {
			return nil, "", common.NewCodedError(common.CodeNotFound, "document not found")
		}
		if f.Type == FileTypeFolder && f.ID == f.ParentID && strings.EqualFold(f.Name, SkillsFolderName) {
			return nil, "", permission.ErrPermissionDenied
		}
		if f.Type == FileTypeFolder {
			if err := s.checkFileTreeAccess(ctx, uid, f, permission.OperationRead); err != nil {
				return nil, "", err
			}
		} else {
			if err := checkFileAccess(ctx, uid, f, permission.OperationRead); err != nil {
				return nil, "", err
			}
		}
		roots = append(roots, f)
	}

	if len(roots) == 0 {
		return nil, "", common.NewCodedError(common.CodeParamError, "ids is required")
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	usedPaths := make(map[string]int)
	visitedFolders := make(map[string]struct{})

	var writeEntry func(f *entity.File, parentZipDir string) error
	writeEntry = func(f *entity.File, parentZipDir string) error {
		if f == nil || f.Type == FileTypeVirtual {
			return nil
		}

		if f.Type == FileTypeFolder {
			if _, visited := visitedFolders[f.ID]; visited {
				return nil
			}
			visitedFolders[f.ID] = struct{}{}

			folderName := sanitizeZipSegment(f.Name, "folder")
			folderZipPath := uniqueZipPath(parentZipDir, folderName, true, usedPaths)

			children, err := s.fileDAO.ListAllFilesByParentID(ctx, dao.DB, f.ID)
			if err != nil {
				return err
			}

			isRootFolder := f.ID == f.ParentID
			writtenChildren := 0
			for _, child := range children {
				if child == nil || child.Type == FileTypeVirtual {
					continue
				}
				if isRootFolder && child.Type == FileTypeFolder && strings.EqualFold(child.Name, SkillsFolderName) {
					continue
				}
				if err := writeEntry(child, folderZipPath); err != nil {
					return err
				}
				writtenChildren++
			}

			if writtenChildren == 0 {
				if _, err := zw.Create(folderZipPath + "/"); err != nil {
					return err
				}
			}
			return nil
		}

		fileName := sanitizeZipSegment(f.Name, "file")
		fileZipPath := uniqueZipPath(parentZipDir, fileName, false, usedPaths)

		blob, err := s.ReadFileBlob(ctx, storageImpl, f)
		if err != nil {
			// Skip unreadable child files inside folders only when part of a
			// larger archive; if a single non-folder file was requested and
			// cannot be read, fail explicitly.
			if len(roots) == 1 && roots[0].Type != FileTypeFolder {
				return err
			}
			return nil
		}

		w, err := zw.Create(fileZipPath)
		if err != nil {
			return err
		}
		if len(blob) > 0 {
			if _, err := w.Write(blob); err != nil {
				return err
			}
		}
		return nil
	}

	for _, root := range roots {
		if err := writeEntry(root, ""); err != nil {
			_ = zw.Close()
			return nil, "", err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, "", err
	}

	archiveName := "files.zip"
	if len(roots) == 1 {
		base := sanitizeZipSegment(strings.TrimPrefix(roots[0].Name, "."), "files")
		if roots[0].Type != FileTypeFolder {
			ext := path.Ext(base)
			if ext != "" {
				base = strings.TrimSuffix(base, ext)
			}
		}
		if base == "" {
			base = "files"
		}
		archiveName = base + ".zip"
	}

	return buf.Bytes(), archiveName, nil
}

func sanitizeZipSegment(name, fallback string) string {
	cleaned := strings.TrimSpace(name)
	cleaned = strings.ReplaceAll(cleaned, "/", "_")
	cleaned = strings.ReplaceAll(cleaned, "\\", "_")
	cleaned = strings.ReplaceAll(cleaned, "..", "_")
	if cleaned == "" || cleaned == "." || cleaned == "_" {
		return fallback
	}
	return cleaned
}

func uniqueZipPath(parentDir, segment string, isDir bool, used map[string]int) string {
	basePath := segment
	if parentDir != "" {
		basePath = parentDir + "/" + segment
	}
	key := strings.ToLower(basePath)
	count := used[key]
	used[key] = count + 1
	if count == 0 {
		return basePath
	}

	if isDir {
		deduped := fmt.Sprintf("%s(%d)", basePath, count)
		used[strings.ToLower(deduped)]++
		return deduped
	}

	ext := path.Ext(segment)
	stem := strings.TrimSuffix(segment, ext)
	dedupedSegment := fmt.Sprintf("%s(%d)%s", stem, count, ext)
	if parentDir != "" {
		deduped := parentDir + "/" + dedupedSegment
		used[strings.ToLower(deduped)]++
		return deduped
	}
	used[strings.ToLower(dedupedSegment)]++
	return dedupedSegment
}


// GetStorageAddress gets storage address for a file (fallback for when direct blob is empty)
// Matches Python's File2DocumentService.get_storage_address function
func (s *FileService) GetStorageAddress(ctx context.Context, fileID string) (*StorageAddress, error) {
	// Get file2document mapping
	f2d, err := s.file2DocumentDAO.GetByFileID(ctx, dao.DB, fileID)
	if err != nil || len(f2d) == 0 {
		return nil, fmt.Errorf("file2document mapping not found")
	}

	// Get the file
	if f2d[0].FileID == nil {
		return nil, fmt.Errorf("file_id is nil in file2document mapping")
	}
	file, err := s.fileDAO.GetByID(ctx, dao.DB, *f2d[0].FileID)
	if err != nil || file == nil {
		return nil, fmt.Errorf("file not found")
	}

	// If source_type is empty or local, return file's parent_id and location
	if file.SourceType == "" || entity.FileSource(file.SourceType) == entity.FileSourceLocal {
		if file.Location == nil || *file.Location == "" {
			return nil, fmt.Errorf("file location is empty")
		}
		return &StorageAddress{
			Bucket: file.ParentID,
			Name:   *file.Location,
		}, nil
	}

	// Otherwise, use document's kb_id and location
	if f2d[0].DocumentID == nil {
		return nil, fmt.Errorf("document_id is required")
	}

	documentDAO := dao.NewDocumentDAO()
	doc, err := documentDAO.GetByID(ctx, dao.DB, *f2d[0].DocumentID)
	if err != nil || doc == nil {
		return nil, fmt.Errorf("document not found")
	}

	if doc.Location == nil || *doc.Location == "" {
		return nil, fmt.Errorf("document location is empty")
	}

	return &StorageAddress{
		Bucket: doc.KbID,
		Name:   *doc.Location,
	}, nil
}

// DownloadAgentFile downloads an agent-generated file directly from MinIO without querying the database.
func (s *FileService) DownloadAgentFile(ctx context.Context, tenantID, location string) ([]byte, error) {
	storageImpl := storage.GetStorageFactory().GetStorage()
	if storageImpl == nil {
		return nil, fmt.Errorf("storage not initialized")
	}

	bucketName := fmt.Sprintf("%s-downloads", tenantID)

	blob, err := storageImpl.Get(ctx, bucketName, location)
	if err != nil {
		return nil, fmt.Errorf("failed to read file from storage: %w", err)
	}

	return blob, nil
}

// GetFileContents fetches file contents (text + image) from storage
// for the given file dicts. Images are always returned as MIME-preserving
// base64 data URIs so the multimodal conversion layer (parseDataURIOrB64)
// accepts them.
//
// File dicts are the descriptors returned by the upload_info endpoint
// (UploadInfos / storeUploadInfoBlob). They contain:
//
//   - "id":         storage location UUID (key in the downloads bucket)
//   - "created_by": the user ID who owns the downloads bucket
//   - "name":       the original filename
//   - "mime_type":  the content type
//
// Blobs are stored directly in "{created_by}-downloads/{id}" in object
// storage WITHOUT a corresponding File entity row in the database.
// Mirrors Python's FileService.get_files → get_blob(user_id, file_id).
func (s *FileService) GetFileContents(ctx context.Context, uid string, fileDicts []map[string]interface{}) (texts []string, images []string, err error) {
	storageImpl := storage.GetStorageFactory().GetStorage()
	if storageImpl == nil {
		return nil, nil, fmt.Errorf("storage not initialized")
	}

	for _, fd := range fileDicts {
		id, _ := fd["id"].(string)
		if id == "" {
			continue
		}
		name, _ := fd["name"].(string)
		mimeType, _ := fd["mime_type"].(string)
		createdBy, _ := fd["created_by"].(string)
		if createdBy == "" {
			createdBy = uid
		}
		// Permission: only the owner can access their uploads bucket.
		if createdBy != uid {
			return nil, nil, fmt.Errorf("no authorization")
		}

		data, derr := storageImpl.Get(ctx, createdBy+"-downloads", id)
		if derr != nil || len(data) == 0 {
			continue
		}

		ft := utility.FilenameType(name)
		if utility.IsVideoFilename(name) {
			// Uploads accept video, but no parser implements video analysis.
			// Report that instead of inlining the container as a bogus image
			// data URI (the old visual classification) or dumping raw bytes.
			texts = append(texts, fmt.Sprintf("\n -----------------\nFile: %s\nContent as following: \n(video content is not supported)\n", name))
			continue
		}
		if ft == utility.FileTypeVISUAL {
			mediaType := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
			if mediaType == "" {
				ext := utility.GetFileExtension(name)
				mediaType = utility.GetContentType(ext, string(ft))
			}
			images = append(images, "data:"+mediaType+";base64,"+base64.StdEncoding.EncodeToString(data))
		} else {
			content := parseFileContent(ctx, name, data)
			texts = append(texts, fmt.Sprintf("\n -----------------\nFile: %s\nContent as following: \n%s", name, content))
		}
	}
	return texts, images, nil
}

// ParseAgentUploads resolves descriptors returned by upload_info from the
// caller's downloads bucket and converts them to sys.files values.
func (s *FileService) ParseAgentUploads(ctx context.Context, userID string, fileDicts []map[string]interface{}, layoutRecognize string) ([]string, error) {
	storageImpl := storage.GetStorageFactory().GetStorage()
	if storageImpl == nil {
		return nil, fmt.Errorf("storage not initialized")
	}

	contents := make([]string, 0, len(fileDicts))
	for i, fd := range fileDicts {
		id, _ := fd["id"].(string)
		name, _ := fd["name"].(string)
		mimeType, _ := fd["mime_type"].(string)
		createdBy, _ := fd["created_by"].(string)
		if id == "" || name == "" || mimeType == "" || createdBy == "" {
			return nil, fmt.Errorf("file %d: id, name, mime_type, and created_by are required", i)
		}
		if createdBy != userID {
			return nil, fmt.Errorf("file %q: created_by does not match the current user", name)
		}

		data, err := storageImpl.Get(ctx, createdBy+"-downloads", id)
		if err != nil {
			return nil, fmt.Errorf("file %q: read upload: %w", name, err)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("file %q: upload is empty", name)
		}

		// Reject video filenames before the MIME-based image branch: a
		// mislabeled video (e.g. clip.mp4 declaring image/png) would otherwise
		// be inlined as a bogus image data URI without ever reaching the
		// parser-level video check.
		if utility.IsVideoFilename(name) {
			return nil, fmt.Errorf("file %q: video content is not supported", name)
		}

		mediaType := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
		if strings.HasPrefix(mediaType, "image/") {
			contents = append(contents, "data:"+mediaType+";base64,"+base64.StdEncoding.EncodeToString(data))
			continue
		}

		content, err := parseAgentUploadContent(ctx, name, data, layoutRecognize)
		if err != nil {
			return nil, fmt.Errorf("file %q: parse upload: %w", name, err)
		}
		contents = append(contents, content)
	}
	return contents, nil
}

func parseAgentUploadContent(ctx context.Context, filename string, data []byte, layoutRecognize string) (string, error) {
	content := string(data)
	fileType := utility.GetFileType(filename)
	if fileType != utility.FileTypeOTHER {
		fp, err := parser.GetParser(fileType)
		if err != nil {
			return "", err
		}
		if configurable, ok := fp.(interface{ ConfigureFromSetup(map[string]any) }); ok {
			configurable.ConfigureFromSetup(map[string]any{"layout_recognize": layoutRecognize})
		}
		res := fp.ParseWithResult(ctx, filename, data)
		if res.Err != nil {
			return "", res.Err
		}
		parsed, err := parseResultText(res)
		if err != nil {
			return "", err
		}
		content = parsed
	}
	return fmt.Sprintf("\n -----------------\nFile: %s\nContent as following: \n%s", filename, content), nil
}

// parseResultText converts a parser result into the readable text expected by
// sys.files. Every parser result is structured JSON, so the items are flattened
// in order, preferring each item's text field and rendering tables; items
// without either are serialized as a final fallback.
func parseResultText(res parser.ParseResult) (string, error) {
	parts := make([]string, 0, len(res.JSON))
	for _, item := range res.JSON {
		m := item.ToMap()
		if text, ok := m["text"].(string); ok {
			parts = append(parts, text)
			continue
		}
		if td, ok := m["table"].(*entity.TableData); ok && td != nil {
			parts = append(parts, tableutil.RenderTableHTML(td))
			continue
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return "", err
		}
		parts = append(parts, string(raw))
	}
	return strings.Join(parts, "\n"), nil
}

// parseFileContent tries to parse a file's contents using the appropriate parser.
// Falls back to returning raw text if no parser is available.
func parseFileContent(ctx context.Context, filename string, data []byte) string {
	fileType := utility.GetFileType(filename)
	if fileType == utility.FileTypeOTHER {
		return string(data)
	}
	fp, err := parser.GetParser(fileType)
	if err != nil {
		return string(data)
	}
	res := fp.ParseWithResult(ctx, filename, data)
	if res.Err != nil {
		return string(data)
	}
	content, err := parseResultText(res)
	if err != nil {
		return string(data)
	}
	return content
}
