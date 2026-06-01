package notifications

import (
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// Helper to create a dummy logger discarding output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Test extracting from D-Bus raw image-data hints.
func TestIconExtractor_ExtractFromHints(t *testing.T) {
	logger := testLogger()
	extractor := NewIconExtractor(logger)

	// 1. Prepare raw 2x2 RGBA pixels (Red, Green, Blue, Alpha)
	rgbaData := []byte{
		255, 0, 0, 255, // Red
		0, 255, 0, 255, // Green
		0, 0, 255, 255, // Blue
		255, 255, 255, 128, // Semitransparent White
	}

	// Structure matching (iiibiiay):
	// width (2), height (2), rowstride (8), has_alpha (true), bits_per_sample (8), channels (4), data
	hintVal := []interface{}{
		int32(2), int32(2), int32(8), true, int32(8), int32(4), rgbaData,
	}
	hints := map[string]dbus.Variant{
		"image-data": dbus.MakeVariant(hintVal),
	}

	base64Str := extractor.Extract("TestApp", "", hints)
	if base64Str == "" {
		t.Fatal("Expected successfully extracted base64 icon from raw RGBA image hint, got empty string")
	}
	if !strings.HasPrefix(base64Str, "data:image/png;base64,") {
		t.Errorf("Expected PNG data URI prefix, got: %s", base64Str)
	}

	// Verify it decodes back to a valid image
	rawEncoded := strings.TrimPrefix(base64Str, "data:image/png;base64,")
	decodedBytes, err := base64.StdEncoding.DecodeString(rawEncoded)
	if err != nil {
		t.Fatalf("Failed to decode base64 result: %v", err)
	}
	img, err := png.Decode(strings.NewReader(string(decodedBytes)))
	if err != nil {
		t.Fatalf("Failed to parse output PNG: %v", err)
	}
	if bounds := img.Bounds(); bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Errorf("Expected output size 2x2, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	// 2. Test RGB (3 channels) raw image mapping
	rgbData := []byte{
		255, 0, 0, // Red
		0, 255, 0, // Green
		0, 0, 255, // Blue
		255, 255, 255, // White
	}
	hintValRGB := []interface{}{
		int32(2), int32(2), int32(6), false, int32(8), int32(3), rgbData,
	}
	hintsRGB := map[string]dbus.Variant{
		"image_data": dbus.MakeVariant(hintValRGB), // alternative key
	}
	base64StrRGB := extractor.Extract("TestApp", "", hintsRGB)
	if base64StrRGB == "" {
		t.Fatal("Expected successfully extracted base64 icon from raw RGB image hint, got empty string")
	}

	// 3. Test validation constraint: oversized dimensions
	hintValOversized := []interface{}{
		int32(600), int32(600), int32(2400), true, int32(8), int32(4), make([]byte, 600*2400),
	}
	hintsOversized := map[string]dbus.Variant{
		"icon_data": dbus.MakeVariant(hintValOversized), // deprecated key
	}
	base64Oversized := extractor.Extract("TestApp", "", hintsOversized)
	if base64Oversized != "" {
		t.Error("Expected resolution failure for oversized raw image, but got a result")
	}
}

// Test extracting from absolute paths and file URIs.
func TestIconExtractor_ExtractFromPath(t *testing.T) {
	logger := testLogger()
	extractor := NewIconExtractor(logger)

	tempDir := t.TempDir()
	tempPng := filepath.Join(tempDir, "test_icon.png")

	// Create a dummy valid PNG file
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	f, err := os.Create(tempPng)
	if err != nil {
		t.Fatalf("Failed to create temp PNG: %v", err)
	}
	_ = png.Encode(f, img)
	f.Close()

	// 1. Extract using direct absolute path
	res1 := extractor.Extract("", tempPng, nil)
	if res1 == "" {
		t.Fatal("Expected base64 output from absolute path, got empty")
	}
	if !strings.HasPrefix(res1, "data:image/png;base64,") {
		t.Errorf("Expected PNG mime prefix, got: %s", res1)
	}

	// 2. Extract using file:// URI schema
	res2 := extractor.Extract("", "file://"+tempPng, nil)
	if res2 == "" {
		t.Fatal("Expected base64 output from file:// URI, got empty")
	}

	// 3. Size limit verification: create file > 150KB
	tempHuge := filepath.Join(tempDir, "huge_icon.png")
	hugeFile, err := os.Create(tempHuge)
	if err != nil {
		t.Fatalf("Failed to create huge file: %v", err)
	}
	_, _ = hugeFile.Write(make([]byte, 160*1024)) // 160 KB
	hugeFile.Close()

	resHuge := extractor.Extract("", tempHuge, nil)
	if resHuge != "" {
		t.Error("Expected extractor to reject file exceeding 150KB size cap, but got output")
	}
}

// Test themed icon resolution via mock environment directories.
func TestIconExtractor_ExtractFromThemedName(t *testing.T) {
	logger := testLogger()
	extractor := NewIconExtractor(logger)

	// Set up a mock environment mimicking standard Linux paths
	tempBase := t.TempDir()

	// Overwrite HOME env so os.UserHomeDir resolves to our mock environment
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tempBase)

	// 1. Create mock system icon pathways under mock $HOME/.local/share/icons
	userIcons := filepath.Join(tempBase, ".local/share/icons")
	err := os.MkdirAll(userIcons, 0755)
	if err != nil {
		t.Fatalf("Failed to create mock icons dir: %v", err)
	}

	// Create dummy resolved icon file
	targetPng := filepath.Join(userIcons, "my_custom_app.png")
	img := image.NewRGBA(image.Rect(0, 0, 5, 5))
	f, err := os.Create(targetPng)
	if err != nil {
		t.Fatalf("Failed to create dummy png: %v", err)
	}
	_ = png.Encode(f, img)
	f.Close()

	// Extract and resolve from themed name
	resThemed := extractor.Extract("", "my_custom_app", nil)
	if resThemed == "" {
		t.Fatal("Expected to resolve themed name my_custom_app under mock icons dir, got empty")
	}

	// 2. Create mock .desktop file under mock $HOME/.local/share/applications/
	userApps := filepath.Join(tempBase, ".local/share/applications")
	err = os.MkdirAll(userApps, 0755)
	if err != nil {
		t.Fatalf("Failed to create mock apps dir: %v", err)
	}

	desktopFile := filepath.Join(userApps, "some_player.desktop")
	desktopContent := `[Desktop Entry]
Name=Some Player
Exec=someplayer
Icon=my_custom_app
Type=Application
`
	err = os.WriteFile(desktopFile, []byte(desktopContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock desktop file: %v", err)
	}

	// Resolve themed icon by parsing desktop file from AppName ("some_player")
	resDesktop := extractor.Extract("some_player", "", nil)
	if resDesktop == "" {
		t.Fatal("Expected to resolve icon name from mock desktop file, got empty")
	}

	// Check cache hit
	cachedRes := extractor.Extract("some_player", "", nil)
	if cachedRes != resDesktop {
		t.Error("Expected cached icon to match initially resolved icon")
	}
}

// Test the pure-Go aspect ratio preserving resizing utility.
func TestIconExtractor_ResizeImage(t *testing.T) {
	logger := testLogger()
	extractor := NewIconExtractor(logger)

	// Create high resolution 200x100 canvas
	img := image.NewNRGBA(image.Rect(0, 0, 200, 100))

	// Downscale maintaining aspect ratio to max 96x96
	resized := extractor.resizeImage(img, 96, 96)
	bounds := resized.Bounds()

	// Original ratio 2.0 (200/100). Max width 96 -> New height should be 48 (96/2.0)
	if bounds.Dx() != 96 || bounds.Dy() != 48 {
		t.Errorf("Expected resized dimensions to preserve 2.0 ratio as 96x48, got %dx%d",
			bounds.Dx(), bounds.Dy())
	}
}
