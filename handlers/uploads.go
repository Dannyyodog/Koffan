package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"

	libheif "github.com/strukturag/libheif-go"
)

// MaxImageSize caps a single uploaded image at 10 MB.
const MaxImageSize int64 = 10 * 1024 * 1024

// allowedImageContentTypes maps accepted upload content types to the canonical
// on-disk extension. HEIC/HEIF are decoded and re-encoded as JPEG, so they map
// to ".jpg" — clients always receive a browser-renderable format.
var allowedImageContentTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
	"image/heic": ".jpg", // re-encoded
	"image/heif": ".jpg", // re-encoded
}

// uploadsRoot is the on-disk directory where uploaded images are stored.
// Set once at startup by InitUploads.
var uploadsRoot string

// Sentinel errors so handlers can map upload failures to i18n error keys.
var (
	errImageTooLarge      = errors.New("image too large")
	errImageInvalidFormat = errors.New("image format not supported")
	errImageDecodeFailed  = errors.New("image decode failed")
)

// InitUploads sets the uploads directory and ensures it exists. Mirrors the
// MkdirAll pattern used by db.Init for the database directory.
func InitUploads(path string) error {
	if path == "" {
		return fmt.Errorf("uploads path is empty")
	}
	uploadsRoot = path
	if err := os.MkdirAll(uploadsRoot, 0755); err != nil {
		log.Printf("[UPLOAD] failed to create uploads directory %q: %v", uploadsRoot, err)
		return err
	}
	log.Printf("[UPLOAD] Uploads directory ready at %q", uploadsRoot)
	return nil
}

// UploadsRoot returns the on-disk uploads directory.
func UploadsRoot() string {
	return uploadsRoot
}

// saveUploadedImage validates a single uploaded image, re-encodes HEIC/HEIF to
// JPEG, deduplicates by content hash, and writes the file under uploadsRoot.
// Returns the bare filename (not a full path).
func saveUploadedImage(file *multipart.FileHeader) (string, error) {
	if file == nil {
		return "", errImageInvalidFormat
	}

	if file.Size > MaxImageSize {
		return "", errImageTooLarge
	}

	contentType := file.Header.Get("Content-Type")
	ext, ok := allowedImageContentTypes[contentType]
	if !ok {
		return "", errImageInvalidFormat
	}

	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("open uploaded file: %w", err)
	}
	defer src.Close()

	// Cap the read at MaxImageSize+1 to defensively reject mismatched Content-Length.
	raw, err := io.ReadAll(io.LimitReader(src, MaxImageSize+1))
	if err != nil {
		return "", fmt.Errorf("read uploaded file: %w", err)
	}
	if int64(len(raw)) > MaxImageSize {
		return "", errImageTooLarge
	}

	finalBytes := raw

	// Re-encode HEIC/HEIF to JPEG quality 85 so clients always get a
	// browser-renderable format. The on-disk extension is forced to .jpg by the
	// allowedImageContentTypes map above.
	if contentType == "image/heic" || contentType == "image/heif" {
		decoded, decErr := decodeHEIC(raw)
		if decErr != nil {
			log.Printf("[UPLOAD] HEIC decode failed: %v", decErr)
			return "", errImageDecodeFailed
		}

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, decoded, &jpeg.Options{Quality: 85}); err != nil {
			log.Printf("[UPLOAD] JPEG re-encode failed: %v", err)
			return "", errImageDecodeFailed
		}
		finalBytes = buf.Bytes()
	}

	sum := sha256.Sum256(finalBytes)
	filename := hex.EncodeToString(sum[:])[:16] + ext

	if uploadsRoot == "" {
		return "", fmt.Errorf("uploads directory not initialized")
	}
	dest := filepath.Join(uploadsRoot, filename)

	// Natural dedup: if a file with this content hash already exists, skip the
	// write. The DB row is the source of truth for which item points at it.
	if _, statErr := os.Stat(dest); statErr == nil {
		return filename, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("stat destination: %w", statErr)
	}

	if err := os.WriteFile(dest, finalBytes, 0644); err != nil {
		return "", fmt.Errorf("write uploaded file: %w", err)
	}

	return filename, nil
}

// decodeHEIC decodes a HEIC/HEIF byte buffer using libheif and returns a
// standard image.Image suitable for re-encoding. libheif-go uses runtime
// finalizers for cleanup, so no explicit Free/Close is required.
func decodeHEIC(raw []byte) (image.Image, error) {
	ctx, err := libheif.NewContext()
	if err != nil {
		return nil, fmt.Errorf("libheif context: %w", err)
	}

	if err := ctx.ReadFromMemory(raw); err != nil {
		return nil, fmt.Errorf("libheif read: %w", err)
	}

	handle, err := ctx.GetPrimaryImageHandle()
	if err != nil {
		return nil, fmt.Errorf("libheif primary handle: %w", err)
	}

	img, err := handle.DecodeImage(libheif.ColorspaceRGB, libheif.ChromaInterleavedRGB, nil)
	if err != nil {
		return nil, fmt.Errorf("libheif decode: %w", err)
	}

	std, err := img.GetImage()
	if err != nil {
		return nil, fmt.Errorf("libheif to image.Image: %w", err)
	}
	return std, nil
}
