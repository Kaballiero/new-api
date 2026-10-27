package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/webp"
)

// File helpers share downloads, decoding and request-local caching.

// getContextCacheKey builds the URL request-cache key.
func getContextCacheKey(url string) string {
	return fmt.Sprintf("file_cache_%s", common.GenerateHMAC(url))
}

// getBase64ContextCacheKey builds the Base64 request-cache key.
// Hash length, MIME and the first 128 characters instead of the full Base64 data.
func getBase64ContextCacheKey(data string, mimeType string) string {
	keyMaterial := fmt.Sprintf("%d:%s:", len(data), mimeType)
	if len(data) > 128 {
		keyMaterial += data[:128]
	} else {
		keyMaterial += data
	}
	return fmt.Sprintf("b64_cache_%s", common.GenerateHMAC(keyMaterial))
}

// LoadFileSource loads file data, sharing cached sources within a request.
func LoadFileSource(c *gin.Context, source types.FileSource, reason ...string) (*types.CachedFileData, error) {
	if source == nil {
		return nil, fmt.Errorf("file source is nil")
	}

	if common.DebugEnabled {
		logger.LogDebug(c, "LoadFileSource starting for: %s", source.GetIdentifier())
	}

	// Check the source cache first.
	if source.HasCache() {
		if c != nil {
			registerSourceForCleanup(c, source)
		}
		return source.GetCache(), nil
	}

	// Lock the source while loading.
	source.Mu().Lock()
	defer source.Mu().Unlock()

	// Recheck the cache under the lock.
	if source.HasCache() {
		if c != nil {
			registerSourceForCleanup(c, source)
		}
		return source.GetCache(), nil
	}

	// Load by source type, including the URL request-cache lookup.
	var cachedData *types.CachedFileData
	var contextKey string
	var err error

	switch s := source.(type) {
	case *types.URLSource:
		if c != nil {
			contextKey = getContextCacheKey(s.URL)
			if cached, exists := c.Get(contextKey); exists {
				data := cached.(*types.CachedFileData)
				source.SetCache(data)
				registerSourceForCleanup(c, source)
				return data, nil
			}
		}
		cachedData, err = loadFromURL(c, s.URL, reason...)
	case *types.Base64Source:
		if c != nil {
			contextKey = getBase64ContextCacheKey(s.Base64Data, s.MimeType)
			if cached, exists := c.Get(contextKey); exists {
				data := cached.(*types.CachedFileData)
				source.SetCache(data)
				registerSourceForCleanup(c, source)
				return data, nil
			}
		}
		cachedData, err = loadFromBase64(s.Base64Data, s.MimeType)
	default:
		return nil, fmt.Errorf("unsupported file source type: %T", source)
	}

	if err != nil {
		return nil, err
	}

	// Cache the loaded data.
	source.SetCache(cachedData)
	if contextKey != "" && c != nil {
		c.Set(contextKey, cachedData)
	}

	// Register the source for cleanup at the end of the request.
	if c != nil {
		registerSourceForCleanup(c, source)
	}

	return cachedData, nil
}

// registerSourceForCleanup registers a file source for request cleanup.
func registerSourceForCleanup(c *gin.Context, source types.FileSource) {
	if source.IsRegistered() {
		return
	}

	key := string(constant.ContextKeyFileSourcesToCleanup)
	var sources []types.FileSource
	if existing, exists := c.Get(key); exists {
		sources = existing.([]types.FileSource)
	}
	sources = append(sources, source)
	c.Set(key, sources)
	source.SetRegistered(true)
}

// CleanupFileSources closes every file source registered for the request.
// Call at the end of the request, normally from middleware.
func CleanupFileSources(c *gin.Context) {
	key := string(constant.ContextKeyFileSourcesToCleanup)
	if sources, exists := c.Get(key); exists {
		for _, source := range sources.([]types.FileSource) {
			if cache := source.GetCache(); cache != nil {
				cache.Close()
			}
		}
		c.Set(key, nil)
	}
}

// loadFromURL downloads a file.
func loadFromURL(c *gin.Context, url string, reason ...string) (*types.CachedFileData, error) {
	// Download the file.
	var maxFileSize = constant.MaxFileDownloadMB * 1024 * 1024

	if common.DebugEnabled {
		logger.LogDebug(c, "loadFromURL: initiating download")
	}
	resp, err := httpclient.DoDownloadRequest(url, reason...)
	if err != nil {
		return nil, fmt.Errorf("failed to download file from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to download file, status code: %d", resp.StatusCode)
	}

	// Read the bounded file content.
	if common.DebugEnabled {
		logger.LogDebug(c, "loadFromURL: reading response body")
	}
	fileBytes, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxFileSize+1)))
	if err != nil {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}
	if len(fileBytes) > maxFileSize {
		return nil, fmt.Errorf("file size exceeds maximum allowed size: %dMB", constant.MaxFileDownloadMB)
	}

	// Encode as Base64.
	base64Data := base64.StdEncoding.EncodeToString(fileBytes)

	// Detect the MIME type.
	mimeType := smartDetectMimeType(resp, url, fileBytes)

	// Choose disk or memory caching.
	base64Size := int64(len(base64Data))
	var cachedData *types.CachedFileData

	if shouldUseDiskCache(base64Size) {
		// Use the disk cache.
		diskPath, err := writeToDiskCache(base64Data)
		if err != nil {
			// Fall back to memory when disk caching fails.
			logger.LogWarn(c, fmt.Sprintf("Failed to write to disk cache, falling back to memory: %v", err))
			cachedData = types.NewMemoryCachedData(base64Data, mimeType, int64(len(fileBytes)))
		} else {
			cachedData = types.NewDiskCachedData(diskPath, mimeType, int64(len(fileBytes)))
			cachedData.DiskSize = base64Size
			cachedData.OnClose = func(size int64) {
				common.DecrementDiskFiles(size)
			}
			common.IncrementDiskFiles(base64Size)
			if common.DebugEnabled {
				logger.LogDebug(c, "File cached to disk: %s, size: %d bytes", diskPath, base64Size)
			}
		}
	} else {
		// Use the memory cache.
		cachedData = types.NewMemoryCachedData(base64Data, mimeType, int64(len(fileBytes)))
	}

	// Try to read the configuration for image content.
	if strings.HasPrefix(mimeType, "image/") {
		if common.DebugEnabled {
			logger.LogDebug(c, "loadFromURL: decoding image config")
		}
		config, format, err := decodeImageConfig(fileBytes)
		if err == nil {
			cachedData.ImageConfig = &config
			cachedData.ImageFormat = format
			// Update the MIME type when image decoding identifies a more precise format.
			if mimeType == "application/octet-stream" || mimeType == "" {
				cachedData.MimeType = "image/" + format
			}
		}
	}

	return cachedData, nil
}

// shouldUseDiskCache checks the shared disk-cache policy.
func shouldUseDiskCache(dataSize int64) bool {
	return common.ShouldUseDiskCache(dataSize)
}

// writeToDiskCache writes Base64 data to the shared disk cache.
func writeToDiskCache(base64Data string) (string, error) {
	return common.WriteDiskCacheFileString(common.DiskCacheTypeFile, base64Data)
}

// smartDetectMimeType detects the MIME type using headers, URL and bytes.
func smartDetectMimeType(resp *http.Response, url string, fileBytes []byte) string {
	// Try the Content-Type header first.
	mimeType := resp.Header.Get("Content-Type")
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	if mimeType != "" && mimeType != "application/octet-stream" {
		return mimeType
	}

	// Try the filename from Content-Disposition.
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		parts := strings.SplitSeq(cd, ";")
		for part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(strings.ToLower(part), "filename=") {
				name := strings.TrimSpace(strings.TrimPrefix(part, "filename="))
				// Remove surrounding quotes.
				if len(name) > 2 && name[0] == '"' && name[len(name)-1] == '"' {
					name = name[1 : len(name)-1]
				}
				if dot := strings.LastIndex(name, "."); dot != -1 && dot+1 < len(name) {
					ext := strings.ToLower(name[dot+1:])
					if ext != "" {
						mt := GetMimeTypeByExtension(ext)
						if mt != "application/octet-stream" {
							return mt
						}
					}
				}
				break
			}
		}
	}

	// Try the extension from the URL path.
	mt := guessMimeTypeFromURL(url)
	if mt != "application/octet-stream" {
		return mt
	}

	// Sniff the content with http.DetectContentType.
	if len(fileBytes) > 0 {
		sniffed := http.DetectContentType(fileBytes)
		if sniffed != "" && sniffed != "application/octet-stream" {
			// Remove an optional charset parameter.
			if idx := strings.Index(sniffed, ";"); idx != -1 {
				sniffed = strings.TrimSpace(sniffed[:idx])
			}
			return sniffed
		}

		// Try HEIF/HEIC detection, which the standard library does not support.
		if heifMime := detectHEIF(fileBytes); heifMime != "" {
			return heifMime
		}
	}

	// Try image decoding to identify the format.
	if len(fileBytes) > 0 {
		if _, format, err := decodeImageConfig(fileBytes); err == nil && format != "" {
			return "image/" + strings.ToLower(format)
		}
	}

	// Use the final fallback.
	return "application/octet-stream"
}

// loadFromBase64 loads a file from Base64 data.
func loadFromBase64(base64String string, providedMimeType string) (*types.CachedFileData, error) {
	var mimeType string
	var cleanBase64 string

	// Handle the data: prefix.
	if strings.HasPrefix(base64String, "data:") {
		before, after, ok := strings.Cut(base64String, ",")
		if ok {
			header := before
			cleanBase64 = after

			if strings.Contains(header, ":") && strings.Contains(header, ";") {
				mimeStart := strings.Index(header, ":") + 1
				mimeEnd := strings.Index(header, ";")
				if mimeStart < mimeEnd {
					mimeType = header[mimeStart:mimeEnd]
				}
			}
		} else {
			cleanBase64 = base64String
		}
	} else {
		cleanBase64 = base64String
	}

	if providedMimeType != "" {
		mimeType = providedMimeType
	}

	decodedData, err := base64.StdEncoding.DecodeString(cleanBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 data: %w", err)
	}

	base64Size := int64(len(cleanBase64))
	var cachedData *types.CachedFileData

	if shouldUseDiskCache(base64Size) {
		diskPath, err := writeToDiskCache(cleanBase64)
		if err != nil {
			cachedData = types.NewMemoryCachedData(cleanBase64, mimeType, int64(len(decodedData)))
		} else {
			cachedData = types.NewDiskCachedData(diskPath, mimeType, int64(len(decodedData)))
			cachedData.DiskSize = base64Size
			cachedData.OnClose = func(size int64) {
				common.DecrementDiskFiles(size)
			}
			common.IncrementDiskFiles(base64Size)
		}
	} else {
		cachedData = types.NewMemoryCachedData(cleanBase64, mimeType, int64(len(decodedData)))
	}

	if mimeType == "" || strings.HasPrefix(mimeType, "image/") {
		config, format, err := decodeImageConfig(decodedData)
		if err == nil {
			cachedData.ImageConfig = &config
			cachedData.ImageFormat = format
			if mimeType == "" {
				cachedData.MimeType = "image/" + format
			}
		}
	}

	return cachedData, nil
}

// GetImageConfig reads the image configuration.
func GetImageConfig(c *gin.Context, source types.FileSource) (image.Config, string, error) {
	cachedData, err := LoadFileSource(c, source, "get_image_config")
	if err != nil {
		return image.Config{}, "", err
	}

	if cachedData.ImageConfig != nil {
		return *cachedData.ImageConfig, cachedData.ImageFormat, nil
	}

	base64Str, err := cachedData.GetBase64Data()
	if err != nil {
		return image.Config{}, "", fmt.Errorf("failed to get base64 data: %w", err)
	}
	decodedData, err := base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		return image.Config{}, "", fmt.Errorf("failed to decode base64 for image config: %w", err)
	}

	config, format, err := decodeImageConfig(decodedData)
	if err != nil {
		return image.Config{}, "", err
	}

	cachedData.ImageConfig = &config
	cachedData.ImageFormat = format

	return config, format, nil
}

// GetBase64Data returns Base64-encoded data.
func GetBase64Data(c *gin.Context, source types.FileSource, reason ...string) (string, string, error) {
	cachedData, err := LoadFileSource(c, source, reason...)
	if err != nil {
		return "", "", err
	}
	base64Str, err := cachedData.GetBase64Data()
	if err != nil {
		return "", "", fmt.Errorf("failed to get base64 data: %w", err)
	}
	return base64Str, cachedData.MimeType, nil
}

// DetectFileType classifies the MIME type.
func DetectFileType(mimeType string) types.FileType {
	if strings.HasPrefix(mimeType, "image/") {
		return types.FileTypeImage
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return types.FileTypeAudio
	}
	if strings.HasPrefix(mimeType, "video/") {
		return types.FileTypeVideo
	}
	return types.FileTypeFile
}

// decodeImageConfig decodes the image configuration from bytes.
func decodeImageConfig(data []byte) (image.Config, string, error) {
	reader := bytes.NewReader(data)

	config, format, err := image.DecodeConfig(reader)
	if err == nil {
		return config, format, nil
	}

	reader.Seek(0, io.SeekStart)
	config, err = webp.DecodeConfig(reader)
	if err == nil {
		return config, "webp", nil
	}

	// Try HEIF/HEIC: parse ISOBMFF ispe box for dimensions
	if heifMime := detectHEIF(data); heifMime != "" {
		formatName := "heif"
		if heifMime == "image/heic" {
			formatName = "heic"
		}
		if w, h, ok := parseHEIFDimensions(data); ok {
			return image.Config{Width: w, Height: h}, formatName, nil
		}
		return image.Config{}, "", fmt.Errorf("failed to decode HEIF/HEIC image dimensions")
	}

	return image.Config{}, "", fmt.Errorf("failed to decode image config: unsupported format")
}

// detectHEIF checks ISOBMFF magic bytes to detect HEIC/HEIF files.
// Returns "image/heic", "image/heif", or "" if not recognized.
func detectHEIF(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	// ISOBMFF: bytes[4:8] must be "ftyp"
	if string(data[4:8]) != "ftyp" {
		return ""
	}
	brand := string(data[8:12])
	switch brand {
	case "heic", "heix", "hevc", "hevx", "heim", "heis":
		return "image/heic"
	case "mif1", "msf1":
		return "image/heif"
	default:
		return ""
	}
}

// parseHEIFDimensions parses ISOBMFF box tree to find the ispe box
// and extract image width/height. Returns (width, height, ok).
func parseHEIFDimensions(data []byte) (int, int, bool) {
	size := len(data)
	if size < 12 {
		return 0, 0, false
	}

	// Walk top-level boxes to find "meta"
	offset := 0
	for offset+8 <= size {
		boxSize := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		boxType := string(data[offset+4 : offset+8])
		headerLen := 8

		if boxSize == 1 {
			// 64-bit extended size
			if offset+16 > size {
				break
			}
			boxSize = int(binary.BigEndian.Uint64(data[offset+8 : offset+16]))
			headerLen = 16
		} else if boxSize == 0 {
			// box extends to end of data
			boxSize = size - offset
		}

		if boxSize < headerLen || offset+boxSize > size {
			break
		}

		if boxType == "meta" {
			// meta is a full box: 4 bytes version/flags after header
			metaData := data[offset+headerLen : offset+boxSize]
			if len(metaData) < 4 {
				return 0, 0, false
			}
			return findISPE(metaData[4:])
		}
		offset += boxSize
	}
	return 0, 0, false
}

// findISPE recursively searches for the ispe box within container boxes.
// Path: meta -> iprp -> ipco -> ispe
func findISPE(data []byte) (int, int, bool) {
	offset := 0
	size := len(data)
	for offset+8 <= size {
		boxSize := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		boxType := string(data[offset+4 : offset+8])
		if boxSize < 8 || offset+boxSize > size {
			break
		}
		content := data[offset+8 : offset+boxSize]
		switch boxType {
		case "iprp", "ipco":
			if w, h, ok := findISPE(content); ok {
				return w, h, true
			}
		case "ispe":
			// ispe is a full box: 4 bytes version/flags, then 4 bytes width, 4 bytes height
			if len(content) >= 12 {
				w := int(binary.BigEndian.Uint32(content[4:8]))
				h := int(binary.BigEndian.Uint32(content[8:12]))
				if w > 0 && h > 0 {
					return w, h, true
				}
			}
		}
		offset += boxSize
	}
	return 0, 0, false
}

// guessMimeTypeFromURL guesses a MIME type from the URL extension.
func guessMimeTypeFromURL(url string) string {
	cleanedURL := url
	if q := strings.Index(cleanedURL, "?"); q != -1 {
		cleanedURL = cleanedURL[:q]
	}

	if slash := strings.LastIndex(cleanedURL, "/"); slash != -1 && slash+1 < len(cleanedURL) {
		last := cleanedURL[slash+1:]
		if dot := strings.LastIndex(last, "."); dot != -1 && dot+1 < len(last) {
			ext := strings.ToLower(last[dot+1:])
			return GetMimeTypeByExtension(ext)
		}
	}

	return "application/octet-stream"
}
