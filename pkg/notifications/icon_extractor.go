package notifications

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// IconExtractor handles tiered resolution, extraction, and encoding of notification icons.
type IconExtractor struct {
	logger *slog.Logger
	cache  sync.Map // maps string (icon name / path) to string (base64 string)
}

// NewIconExtractor instantiates a thread-safe IconExtractor.
func NewIconExtractor(logger *slog.Logger) *IconExtractor {
	return &IconExtractor{
		logger: logger,
	}
}

// Extract attempts to resolve and Base64-encode the notification icon from tiered sources.
func (e *IconExtractor) Extract(appName, iconName string, hints map[string]dbus.Variant) string {
	// Tier 1: Intercept raw pixel image-data hints (highest priority, highly dynamic)
	if base64Str := e.ExtractFromHints(hints); base64Str != "" {
		return base64Str
	}

	// Tier 2: Check absolute paths or file URIs in iconName
	if base64Str := e.ExtractFromPath(iconName); base64Str != "" {
		return base64Str
	}

	// Check image-path or image_path D-Bus hints
	for _, hintKey := range []string{"image-path", "image_path"} {
		if val, ok := hints[hintKey]; ok {
			if pathStr, ok := val.Value().(string); ok && pathStr != "" {
				if base64Str := e.ExtractFromPath(pathStr); base64Str != "" {
					return base64Str
				}
			}
		}
	}

	// Tier 3: Resolve themed icon names (e.g. "slack")
	if base64Str := e.ExtractFromThemedName(appName, iconName); base64Str != "" {
		return base64Str
	}

	return ""
}

// ExtractFromHints decodes raw D-Bus pixel arrays (image-data / icon_data) into Base64 PNGs.
func (e *IconExtractor) ExtractFromHints(hints map[string]dbus.Variant) string {
	var rawImage dbus.Variant
	var ok bool

	// Query standard D-Bus notification image hint keys
	if rawImage, ok = hints["image-data"]; !ok {
		if rawImage, ok = hints["image_data"]; !ok {
			rawImage, ok = hints["icon_data"]
		}
	}

	if !ok {
		return ""
	}

	// Freedesktop struct spec: (iiibiiay)
	// [width, height, rowstride, has_alpha, bits_per_sample, channels, data]
	val := rawImage.Value()
	slice, ok := val.([]interface{})
	if !ok || len(slice) < 7 {
		return ""
	}

	w, ok1 := slice[0].(int32)
	h, ok2 := slice[1].(int32)
	rs, ok3 := slice[2].(int32)
	_, ok4 := slice[3].(bool)
	bits, ok5 := slice[4].(int32)
	channels, ok6 := slice[5].(int32)
	data, ok7 := slice[6].([]byte)

	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 {
		return ""
	}

	width := int(w)
	height := int(h)
	rowstride := int(rs)

	// Security Constraint: Reject massive canvases to avoid memory spikes/exhaustion
	if width <= 0 || height <= 0 || width > 512 || height > 512 || bits != 8 {
		e.logger.Warn("Skipping dynamic notification image hint due to validation failure",
			"width", width, "height", height, "bits", bits)
		return ""
	}

	if len(data) < height*rowstride {
		return ""
	}

	// Convert raw pixels to Go NRGBA image
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcIdx := y*rowstride + x*int(channels)
			if srcIdx+int(channels) > len(data) {
				continue
			}
			destIdx := img.PixOffset(x, y)

			if channels == 4 {
				img.Pix[destIdx] = data[srcIdx]     // R
				img.Pix[destIdx+1] = data[srcIdx+1] // G
				img.Pix[destIdx+2] = data[srcIdx+2] // B
				img.Pix[destIdx+3] = data[srcIdx+3] // A (Alpha)
			} else if channels == 3 {
				img.Pix[destIdx] = data[srcIdx]     // R
				img.Pix[destIdx+1] = data[srcIdx+1] // G
				img.Pix[destIdx+2] = data[srcIdx+2] // B
				img.Pix[destIdx+3] = 255            // A (Opaque)
			} else {
				return "" // Unsupported channel configurations (e.g. grayscale)
			}
		}
	}

	// Aspect-ratio preserving downscale to 96x96 to save bandwidth
	var finalImg image.Image = img
	if width > 96 || height > 96 {
		finalImg = e.resizeImage(img, 96, 96)
	}

	// Compress to PNG bytes
	var buf bytes.Buffer
	if err := png.Encode(&buf, finalImg); err != nil {
		e.logger.Error("Failed to encode extracted raw notification image to PNG", "error", err)
		return ""
	}

	// Security Constraint: Enforce strict payload size limit (150 KB)
	if buf.Len() > 153600 {
		e.logger.Warn("Omitting resolved dynamic icon exceeding payload limit", "size", buf.Len())
		return ""
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// ExtractFromPath reads, validates, and Base64-encodes absolute local file paths or file URIs.
func (e *IconExtractor) ExtractFromPath(path string) string {
	if path == "" {
		return ""
	}

	// Clear standard file URI prefix if present
	if strings.HasPrefix(path, "file://") {
		path = strings.TrimPrefix(path, "file://")
	}

	// Verify the path is absolute and exists
	if !filepath.IsAbs(path) {
		return ""
	}

	// Check cache first
	if cachedVal, ok := e.cache.Load(path); ok {
		return cachedVal.(string)
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}

	// Security Constraint: Restrict file read to reasonable limits (150 KB)
	if info.Size() > 153600 {
		e.logger.Warn("Skipping notification icon file exceeding payload size limit", "path", path, "size", info.Size())
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	base64Data := base64.StdEncoding.EncodeToString(data)
	ext := strings.ToLower(filepath.Ext(path))
	var mimeType string
	switch ext {
	case ".svg":
		mimeType = "data:image/svg+xml;base64,"
	case ".png":
		mimeType = "data:image/png;base64,"
	case ".jpg", ".jpeg":
		mimeType = "data:image/jpeg;base64,"
	default:
		mimeType = "data:image/png;base64,"
	}

	result := mimeType + base64Data
	e.cache.Store(path, result)
	return result
}

// ExtractFromThemedName maps a themed app icon name to standard system icon libraries.
func (e *IconExtractor) ExtractFromThemedName(appName, iconName string) string {
	if iconName == "" && appName == "" {
		return ""
	}

	cacheKey := appName + ":" + iconName
	if cachedVal, ok := e.cache.Load(cacheKey); ok {
		return cachedVal.(string)
	}

	resolvedPath := ""

	// 1. Try parsing application desktop entry for custom icon strings
	if appName != "" {
		iconFromDesktop := e.findIconInDesktop(appName)
		if iconFromDesktop != "" {
			if filepath.IsAbs(iconFromDesktop) {
				resolvedPath = iconFromDesktop
			} else {
				resolvedPath = e.findIconInSystem(iconFromDesktop)
			}
		}
	}

	// 2. Fall back to searching for iconName in system icon themes directly
	if resolvedPath == "" && iconName != "" {
		resolvedPath = e.findIconInSystem(iconName)
	}

	if resolvedPath != "" {
		if result := e.ExtractFromPath(resolvedPath); result != "" {
			e.cache.Store(cacheKey, result)
			return result
		}
	}

	return ""
}

// findIconInDesktop searches XDG application entries to locate an icon name mapping.
func (e *IconExtractor) findIconInDesktop(appName string) string {
	var searchDirs []string
	if homeDir, err := os.UserHomeDir(); err == nil {
		searchDirs = append(searchDirs, filepath.Join(homeDir, ".local/share/applications"))
	}
	searchDirs = append(searchDirs, "/usr/local/share/applications", "/usr/share/applications")

	appNameLower := strings.ToLower(appName)
	for _, dir := range searchDirs {
		for _, name := range []string{appNameLower, appName} {
			desktopPath := filepath.Join(dir, name+".desktop")
			if icon := e.parseIconFromDesktopFile(desktopPath); icon != "" {
				return icon
			}
		}

		// Failback to reading all files matching search string
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".desktop") {
				continue
			}
			if strings.Contains(strings.ToLower(f.Name()), appNameLower) {
				desktopPath := filepath.Join(dir, f.Name())
				if icon := e.parseIconFromDesktopFile(desktopPath); icon != "" {
					return icon
				}
			}
		}
	}
	return ""
}

// parseIconFromDesktopFile parses a single desktop entry to read its Icon value.
func (e *IconExtractor) parseIconFromDesktopFile(desktopPath string) string {
	info, err := os.Stat(desktopPath)
	if err != nil {
		return ""
	}
	if info.Size() > 65536 { // 64KB Security limit
		return ""
	}

	file, err := os.Open(desktopPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inDesktopEntrySection := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[Desktop Entry]" {
			inDesktopEntrySection = true
			continue
		} else if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDesktopEntrySection = false
			continue
		}

		if inDesktopEntrySection && strings.HasPrefix(line, "Icon=") {
			return strings.TrimPrefix(line, "Icon=")
		}
	}
	return ""
}

// findIconInSystem searches standard theme libraries (e.g. /usr/share/icons) to resolve matching assets.
func (e *IconExtractor) findIconInSystem(icon string) string {
	if icon == "" {
		return ""
	}

	var searchDirs []string
	if homeDir, err := os.UserHomeDir(); err == nil {
		searchDirs = append(searchDirs, filepath.Join(homeDir, ".local/share/icons"))
	}
	searchDirs = append(searchDirs, "/usr/local/share/icons", "/usr/share/icons")

	// 1. Direct search flat /usr/share/pixmaps fallback directory
	for _, ext := range []string{".png", ".svg"} {
		path := filepath.Join("/usr/share/pixmaps", icon+ext)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}

	// 2. Scan standard icon directories targeting common theme directories
	for _, dir := range searchDirs {
		for _, ext := range []string{".png", ".svg"} {
			// Scan hicolor theme apps category first (very standard for desktop apps)
			pattern := filepath.Join(dir, "hicolor", "*", "apps", icon+ext)
			matches, err := filepath.Glob(pattern)
			if err == nil && len(matches) > 0 {
				return matches[0]
			}
		}

		// Fallback shallow walk searching system themes
		// Capped to first 300 visited files for performance security
		var foundPath string
		visitCount := 0
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			visitCount++
			if visitCount > 300 {
				return filepath.SkipDir
			}
			if info.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			if base == icon+".png" || base == icon+".svg" {
				foundPath = path
				return filepath.SkipDir
			}
			return nil
		})
		if foundPath != "" {
			return foundPath
		}
	}

	return ""
}

// resizeImage downscales NRGBA images keeping their original aspect ratio via nearest-neighbor mapping.
func (e *IconExtractor) resizeImage(img image.Image, maxW, maxH int) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= maxW && h <= maxH {
		return img
	}

	ratio := float64(w) / float64(h)
	newW, newH := maxW, maxH
	if ratio > 1.0 {
		newH = int(float64(maxW) / ratio)
	} else {
		newW = int(float64(maxH) * ratio)
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	resized := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := int(float64(x) * float64(w) / float64(newW))
			srcY := int(float64(y) * float64(h) / float64(newH))
			resized.Set(x, y, img.At(srcX, srcY))
		}
	}
	return resized
}
