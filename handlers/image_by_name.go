package handlers

import (
	"errors"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"shopping-list/db"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// UploadImageByName uploads an image and binds it to an item name (case-insensitive).
// Used by the recipe detail page so a user can attach a photo to an ingredient
// without first creating a shopping-list item with that name. The same row in
// item_history.image_path then powers BOTH the recipe ingredient thumbnail AND
// any shopping-list item with the matching name — they always stay in sync.
//
// URL: POST /image-by-name/:name (name is URL-encoded)
// Form: image=<file>
// Response: JSON {"name":"...","image_url":"/uploads/<file>"}
// Broadcast: item_image_updated {name, image_url} — same event used by item
//   image upload, so list views and recipe views update via the same dispatcher.
func UploadImageByName(c *fiber.Ctx) error {
	name, ok := decodeNameParam(c)
	if !ok {
		return sendError(c, 400, "error.name_required")
	}

	file, err := c.FormFile("image")
	if err != nil || file == nil {
		return sendError(c, 400, "error.image_required")
	}

	filename, err := saveUploadedImage(file)
	if err != nil {
		switch {
		case errors.Is(err, errImageTooLarge):
			return sendError(c, 413, "error.image_too_large")
		case errors.Is(err, errImageInvalidFormat):
			return sendError(c, 415, "error.image_invalid_format")
		case errors.Is(err, errImageDecodeFailed):
			return sendError(c, 422, "error.image_decode_failed")
		default:
			log.Printf("[UPLOAD] image-by-name save failed: %v", err)
			return sendError(c, 500, "error.image_save_failed")
		}
	}

	if err := db.UpsertItemImage(name, filename); err != nil {
		log.Printf("[UPLOAD] upsert image for %q failed: %v", name, err)
		return sendError(c, 500, "error.image_save_failed")
	}

	url := "/uploads/" + filename
	BroadcastUpdate("item_image_updated", map[string]interface{}{
		"name":      name,
		"image_url": url,
	})

	return c.JSON(fiber.Map{
		"name":      name,
		"image_url": url,
	})
}

// DeleteImageByName clears the image binding for an item name and removes the
// file from disk. Broadcasts item_image_updated with empty image_url so all
// listening views revert to placeholders.
func DeleteImageByName(c *fiber.Ctx) error {
	name, ok := decodeNameParam(c)
	if !ok {
		return sendError(c, 400, "error.name_required")
	}

	oldPath, err := db.DeleteItemImage(name)
	if err != nil {
		log.Printf("[UPLOAD] image-by-name delete failed: %v", err)
		return sendError(c, 500, "error.image_save_failed")
	}

	if oldPath != "" && UploadsRoot() != "" {
		fullPath := filepath.Join(UploadsRoot(), oldPath)
		if rmErr := os.Remove(fullPath); rmErr != nil && !os.IsNotExist(rmErr) {
			log.Printf("[UPLOAD] remove file %q failed: %v", fullPath, rmErr)
		}
	}

	BroadcastUpdate("item_image_updated", map[string]interface{}{
		"name":      name,
		"image_url": "",
	})

	return c.SendStatus(204)
}

// decodeNameParam extracts and URL-decodes the :name path param. Returns false
// if the result is empty/whitespace.
func decodeNameParam(c *fiber.Ctx) (string, bool) {
	raw := c.Params("name")
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", false
	}
	return decoded, true
}
