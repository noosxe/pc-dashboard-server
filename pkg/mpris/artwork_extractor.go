package mpris

import (
	"encoding/base64"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ArtworkExtractor resolves local artwork files (file:// URIs or absolute paths)
// and encodes them into Base64-encoded Data URLs with size-checking and caching.
type ArtworkExtractor struct {
	logger *slog.Logger
	cache  sync.Map // maps string (absolute cleaned path) to string (Base64 Data URL)
}

// NewArtworkExtractor instantiates a thread-safe ArtworkExtractor.
func NewArtworkExtractor(logger *slog.Logger) *ArtworkExtractor {
	return &ArtworkExtractor{
		logger: logger,
	}
}

// Extract attempts to convert a local file URI or absolute path to a Base64-encoded data URL.
// If the path is not local, or if it is invalid/exceeds security boundaries, it returns the input string or empty.
func (e *ArtworkExtractor) Extract(artURL string) string {
	if artURL == "" {
		return ""
	}

	isLocal := false
	localPath := artURL

	// 1. Identify local URI schemas or absolute paths
	if strings.HasPrefix(artURL, "file://") {
		isLocal = true
		localPath = strings.TrimPrefix(artURL, "file://")
	} else if strings.HasPrefix(artURL, "file:") {
		isLocal = true
		localPath = strings.TrimPrefix(artURL, "file:")
	} else if filepath.IsAbs(artURL) {
		isLocal = true
	}

	if !isLocal {
		// Remote URL (e.g. http://, https://) or unrecognized pattern: return as-is
		return artURL
	}

	// 2. Standardize, decode, and clean the local filesystem path
	unescapedPath, err := url.PathUnescape(localPath)
	if err == nil {
		localPath = unescapedPath
	}
	cleanPath := filepath.Clean(localPath)

	// 3. Query the cache to avoid redundant disk and encoding I/O
	if cachedVal, ok := e.cache.Load(cleanPath); ok {
		return cachedVal.(string)
	}

	// 4. Verify path existence and ensure it is not a directory
	// First check if the parent directory exists. If it does not, skip the retry loop immediately.
	parentDir := filepath.Dir(cleanPath)
	if _, err := os.Stat(parentDir); err != nil {
		e.logger.Debug("Parent directory of local artwork does not exist, skipping extraction", "path", cleanPath, "parent", parentDir)
		return ""
	}

	// Retry loop handles the race condition where the browser fires the D-Bus signal
	// before the asynchronous file write of the cover art is completed and flushed.
	var info os.FileInfo
	var statErr error
	for i := 0; i < 6; i++ {
		info, statErr = os.Stat(cleanPath)
		if statErr == nil && info.Size() > 0 && !info.IsDir() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if statErr != nil {
		e.logger.Debug("Local artwork file could not be accessed after retries", "path", cleanPath, "error", statErr)
		return ""
	}

	if info.IsDir() {
		e.logger.Debug("Local artwork path points to a directory, skipping", "path", cleanPath)
		return ""
	}

	// 5. Security Constraint: Enforce a strict file-size cap (2 MB) to prevent memory exhaustion
	if info.Size() > 2097152 {
		e.logger.Warn("Local artwork file exceeds security payload size limit (2 MB)", "path", cleanPath, "size", info.Size())
		return ""
	}

	// 6. Read file from disk
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		e.logger.Error("Failed to read local artwork file from disk", "path", cleanPath, "error", err)
		return ""
	}

	// 7. Resolve appropriate MIME type based on file extension
	ext := strings.ToLower(filepath.Ext(cleanPath))
	var mimeType string
	switch ext {
	case ".png":
		mimeType = "data:image/png;base64,"
	case ".jpg", ".jpeg":
		mimeType = "data:image/jpeg;base64,"
	case ".svg":
		mimeType = "data:image/svg+xml;base64,"
	case ".gif":
		mimeType = "data:image/gif;base64,"
	case ".webp":
		mimeType = "data:image/webp;base64,"
	default:
		mimeType = "data:image/png;base64," // default fallback
	}

	// 8. Base64-encode and prepend MIME prefix
	base64Str := base64.StdEncoding.EncodeToString(data)
	result := mimeType + base64Str

	// 9. Persist to cache
	e.cache.Store(cleanPath, result)

	e.logger.Debug("Successfully extracted and Base64-encoded local artwork", "path", cleanPath, "mime", mimeType, "size_bytes", len(result))
	return result
}
