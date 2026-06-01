package mpris

import (
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtworkExtractor_RemoteURL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	extractor := NewArtworkExtractor(logger)

	inputs := []string{
		"http://images.example.com/album.jpg",
		"https://i.scdn.co/image/ab67616d0000b27382b2",
		"https://example.com/track.png",
	}

	for _, input := range inputs {
		res := extractor.Extract(input)
		if res != input {
			t.Errorf("Expected remote URL to be passed through unmodified, got %q for input %q", res, input)
		}
	}
}

func TestArtworkExtractor_NonExistentFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	extractor := NewArtworkExtractor(logger)

	inputs := []string{
		"file:///non/existent/file.png",
		"file:/another/missing/file.jpg",
		"/absolute/path/to/nothing.png",
	}

	for _, input := range inputs {
		res := extractor.Extract(input)
		if res != "" {
			t.Errorf("Expected empty string for non-existent file path, got %q for input %q", res, input)
		}
	}
}

func TestArtworkExtractor_LocalFileAndMimes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	extractor := NewArtworkExtractor(logger)

	// Create a temp directory for test files
	tempDir, err := os.MkdirTemp("", "artwork_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testCases := []struct {
		filename       string
		content        []byte
		expectedPrefix string
	}{
		{"cover.png", []byte("fake png content"), "data:image/png;base64,"},
		{"cover.jpg", []byte("fake jpeg content"), "data:image/jpeg;base64,"},
		{"cover.jpeg", []byte("fake jpeg content"), "data:image/jpeg;base64,"},
		{"cover.svg", []byte("<svg></svg>"), "data:image/svg+xml;base64,"},
		{"cover.gif", []byte("fake gif content"), "data:image/gif;base64,"},
		{"cover.webp", []byte("fake webp content"), "data:image/webp;base64,"},
		{"cover.unknown", []byte("unknown format"), "data:image/png;base64,"}, // default fallback
	}

	for _, tc := range testCases {
		filePath := filepath.Join(tempDir, tc.filename)
		err := os.WriteFile(filePath, tc.content, 0644)
		if err != nil {
			t.Fatalf("Failed to write test file %s: %v", tc.filename, err)
		}

		// Test using direct absolute path
		res1 := extractor.Extract(filePath)
		if !strings.HasPrefix(res1, tc.expectedPrefix) {
			t.Errorf("For file %s, expected base64 data URL to start with prefix %q, got %q", tc.filename, tc.expectedPrefix, res1)
		}

		// Test using file:// prefix
		fileURIPrefix := "file://" + filePath
		res2 := extractor.Extract(fileURIPrefix)
		if res2 != res1 {
			t.Errorf("Expected file:// prefix result to match absolute path result. got %q vs %q", res2, res1)
		}

		// Test using file: prefix
		fileURI := "file:" + filePath
		res3 := extractor.Extract(fileURI)
		if res3 != res1 {
			t.Errorf("Expected file: prefix result to match absolute path result. got %q vs %q", res3, res1)
		}
	}
}

func TestArtworkExtractor_SizeCapLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	extractor := NewArtworkExtractor(logger)

	tempDir, err := os.MkdirTemp("", "artwork_test_size_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a file within the limit (1.9 MB)
	okFile := filepath.Join(tempDir, "ok.png")
	okContent := make([]byte, 1900*1024)
	if err := os.WriteFile(okFile, okContent, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	resOk := extractor.Extract(okFile)
	if resOk == "" {
		t.Error("Expected successful extraction for file within limit (1.9 MB), but got empty string")
	}

	// Create a file exceeding the limit (2.1 MB)
	largeFile := filepath.Join(tempDir, "large.png")
	largeContent := make([]byte, 2100*1024)
	if err := os.WriteFile(largeFile, largeContent, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	resLarge := extractor.Extract(largeFile)
	if resLarge != "" {
		t.Errorf("Expected empty string for file exceeding limit (2.1 MB), but got %q", resLarge)
	}
}

func TestArtworkExtractor_PercentEncodedPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	extractor := NewArtworkExtractor(logger)

	tempDir, err := os.MkdirTemp("", "artwork test space_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "cover art.png")
	content := []byte("space path artwork")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test unescaping by passing a percent-encoded file:// URL (spaces as %20)
	percentEncodedURI := "file://" + strings.ReplaceAll(filePath, " ", "%20")
	res := extractor.Extract(percentEncodedURI)
	if res == "" {
		t.Fatal("Expected successful extraction for percent-encoded path, got empty string")
	}

	expectedPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(res, expectedPrefix) {
		t.Errorf("Expected extracted base64 data to have prefix %q, got %q", expectedPrefix, res)
	}
}

func TestArtworkExtractor_CachingLogic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	extractor := NewArtworkExtractor(logger)

	tempDir, err := os.MkdirTemp("", "artwork_test_cache_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "caching.png")
	content1 := []byte("original content")
	if err := os.WriteFile(filePath, content1, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// First extraction (reads from disk)
	res1 := extractor.Extract(filePath)
	if res1 == "" {
		t.Fatal("Expected successful first extraction")
	}

	// Modify file content on disk
	content2 := []byte("modified content which is completely different")
	if err := os.WriteFile(filePath, content2, 0644); err != nil {
		t.Fatalf("Failed to overwrite test file: %v", err)
	}

	// Second extraction (should return cached value of content1, ignoring modified content2)
	res2 := extractor.Extract(filePath)
	if res2 != res1 {
		t.Errorf("Expected cached result to be returned on second call. got %q, expected %q", res2, res1)
	}

	// Verify that the cached result indeed contains the base64 of content1
	expectedBase64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(content1)
	if res2 != expectedBase64 {
		t.Errorf("Expected base64 content to represent original file content. got %q, expected %q", res2, expectedBase64)
	}
}
