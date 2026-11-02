package githubclient

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

const (
	MaxCompressedArchiveBytes   int64 = 50 * 1024 * 1024  // 50 MiB
	MaxUncompressedArchiveBytes int64 = 100 * 1024 * 1024 // 100 MiB
	MaxSinglePythonFileBytes    int64 = 1 * 1024 * 1024   // 1 MiB
	MaxTotalPythonSourceBytes   int64 = 20 * 1024 * 1024  // 20 MiB
	MaxPythonFileCount          int   = 2000
	MaxArchiveEntries           int   = 25000
)

type ArchiveLimits struct {
	MaxCompressedBytes        int64
	MaxUncompressedTotalBytes int64
	MaxSingleFileBytes        int64
	MaxTotalPythonBytes       int64
	MaxPythonFiles            int
	MaxEntries                int
}

func DefaultArchiveLimits() ArchiveLimits {
	return ArchiveLimits{
		MaxCompressedBytes:        MaxCompressedArchiveBytes,
		MaxUncompressedTotalBytes: MaxUncompressedArchiveBytes,
		MaxSingleFileBytes:        MaxSinglePythonFileBytes,
		MaxTotalPythonBytes:       MaxTotalPythonSourceBytes,
		MaxPythonFiles:            MaxPythonFileCount,
		MaxEntries:                MaxArchiveEntries,
	}
}

type ExtractedFile struct {
	Path    string
	Content []byte
}

// ExtractPythonFilesFromZip reads a ZIP archive in memory with strict size, count,
// symlink, and path traversal guards, returning only regular .py files.
func ExtractPythonFilesFromZip(r io.Reader, limits ArchiveLimits) ([]ExtractedFile, error) {
	if limits.MaxCompressedBytes <= 0 {
		limits = DefaultArchiveLimits()
	}

	limitedCompressed := io.LimitReader(r, limits.MaxCompressedBytes+1)
	rawZip, err := io.ReadAll(limitedCompressed)
	if err != nil {
		return nil, domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "ZIPアーカイブの読み込みに失敗しました", err)
	}
	if int64(len(rawZip)) > limits.MaxCompressedBytes {
		return nil, domain.NewAppError(domain.ErrCodeRepoTooLarge, http.StatusRequestEntityTooLarge, "ZIPの圧縮サイズが上限（50MiB）を超えています", nil)
	}

	zr, err := zip.NewReader(bytes.NewReader(rawZip), int64(len(rawZip)))
	if err != nil {
		return nil, domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "不正なZIPアーカイブ形式です", err)
	}

	if len(zr.File) > limits.MaxEntries {
		return nil, domain.NewAppError(domain.ErrCodeRepoTooLarge, http.StatusRequestEntityTooLarge, "ZIP内のエントリ数が上限を超えています", nil)
	}

	var totalUncompressedDeclared uint64
	var totalPythonBytes int64
	var files []ExtractedFile
	seenPaths := make(map[string]bool)

	for _, f := range zr.File {
		// Validate entry path before anything else
		relPath, err := sanitizeZipEntryPath(f.Name)
		if err != nil {
			return nil, err
		}

		mode := f.Mode()

		// Accumulate declared uncompressed size across the entire archive
		totalUncompressedDeclared += f.UncompressedSize64
		if totalUncompressedDeclared > uint64(limits.MaxUncompressedTotalBytes) {
			return nil, domain.NewAppError(domain.ErrCodeRepoTooLarge, http.StatusRequestEntityTooLarge, "ZIPの展開後合計サイズが上限（100MiB）を超えています", nil)
		}

		if f.FileInfo().IsDir() || relPath == "" {
			continue
		}
		if !strings.HasSuffix(relPath, ".py") {
			continue
		}

		// Reject .py symlinks explicitly
		if mode&os.ModeSymlink != 0 {
			return nil, domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, fmt.Sprintf("ZIP内にシンボリックリンクのPythonファイルが含まれています: %s", f.Name), nil)
		}
		if !mode.IsRegular() {
			continue
		}

		if len(files) >= limits.MaxPythonFiles {
			return nil, domain.NewAppError(domain.ErrCodeRepoTooLarge, http.StatusRequestEntityTooLarge, fmt.Sprintf("Pythonファイル数が上限（%d件）を超えています", limits.MaxPythonFiles), nil)
		}

		if f.UncompressedSize64 > uint64(limits.MaxSingleFileBytes) {
			return nil, domain.NewAppError(domain.ErrCodeRepoTooLarge, http.StatusRequestEntityTooLarge, fmt.Sprintf("ファイル %s のサイズが上限（1MiB）を超えています", relPath), nil)
		}

		rc, err := f.Open()
		if err != nil {
			return nil, domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "ZIP内ファイルの展開に失敗しました", err)
		}

		content, readErr := io.ReadAll(io.LimitReader(rc, limits.MaxSingleFileBytes+1))
		rc.Close()
		if readErr != nil {
			return nil, domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "ZIP内ファイルの読み取りに失敗しました", readErr)
		}

		// Guard against ZIP bomb lying about UncompressedSize64 header
		if int64(len(content)) > limits.MaxSingleFileBytes {
			return nil, domain.NewAppError(domain.ErrCodeRepoTooLarge, http.StatusRequestEntityTooLarge, fmt.Sprintf("ファイル %s の展開サイズが上限（1MiB）を超えています", relPath), nil)
		}

		totalPythonBytes += int64(len(content))
		if totalPythonBytes > limits.MaxTotalPythonBytes {
			return nil, domain.NewAppError(domain.ErrCodeRepoTooLarge, http.StatusRequestEntityTooLarge, "Pythonソースコードの合計サイズが上限（20MiB）を超えています", nil)
		}

		if seenPaths[relPath] {
			continue
		}
		seenPaths[relPath] = true
		files = append(files, ExtractedFile{
			Path:    relPath,
			Content: content,
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}

// sanitizeZipEntryPath validates a ZIP entry name against Zip Slip, absolute paths,
// backslashes, and strips the single top-level GitHub archive wrapper directory if present.
func sanitizeZipEntryPath(rawName string) (string, error) {
	if rawName == "" {
		return "", domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "空のZIPエントリパスです", nil)
	}
	if strings.ContainsRune(rawName, '\x00') || strings.ContainsRune(rawName, '\\') {
		return "", domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "不正な文字を含むZIPパスです", nil)
	}
	if strings.HasPrefix(rawName, "/") {
		return "", domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "絶対パスのZIPエントリは拒否されました", nil)
	}

	segments := strings.Split(rawName, "/")
	for _, seg := range segments {
		if seg == ".." || seg == "." {
			return "", domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "パストラバーサルを含むZIPエントリは拒否されました", nil)
		}
	}

	cleaned := path.Clean(rawName)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "不正な正規化ZIPパスです", nil)
	}

	// GitHub zipball puts everything under "{owner}-{repo}-{sha}/..."
	// If there is a slash, strip the first directory component; also support flat test fixtures if needed.
	parts := strings.Split(cleaned, "/")
	if len(parts) == 1 {
		if strings.HasSuffix(rawName, "/") {
			// Top-level directory entry itself
			return "", nil
		}
		return parts[0], nil
	}

	rel := strings.Join(parts[1:], "/")
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", domain.NewAppError(domain.ErrCodeInvalidArchive, http.StatusBadRequest, "不正な相対ZIPパスです", nil)
	}
	return rel, nil
}
