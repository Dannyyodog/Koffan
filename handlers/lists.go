package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"shopping-list/db"
	"shopping-list/i18n"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Input length limits
const (
	MaxListNameLength    = 100
	MaxIconLength        = 20 // emoji can be multi-byte
	MaxSectionNameLength = 100
	MaxItemNameLength    = 200
	MaxDescriptionLength = 500
)

// GetListsPage returns the homepage with all lists
func GetListsPage(c *fiber.Ctx) error {
	lists, err := db.GetAllLists()
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}

	templates, _ := db.GetAllTemplates()

	return c.Render("home", fiber.Map{
		"Lists":        lists,
		"Templates":    templates,
		"Translations": i18n.GetAllLocales(),
		"Locales":      i18n.AvailableLocales(),
		"DefaultLang":  i18n.GetDefaultLang(),
	})
}

// GetListView returns a single list with its items
func GetListView(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Redirect("/")
	}

	list, err := db.GetListByID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			// List not found - redirect to home
			return c.Redirect("/")
		}
		// Database error - log and show error
		log.Printf("Error fetching list %d: %v", id, err)
		return sendError(c, 500, "error.database_error")
	}

	// Set this list as active
	db.SetActiveList(id)

	sections, err := db.GetSectionsByList(id)
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}

	stats := db.GetListStats(id)
	lists, _ := db.GetAllLists()

	return c.Render("list", fiber.Map{
		"List":          list,
		"Lists":         lists,
		"Sections":      sections,
		"Stats":         stats,
		"ShowCompleted": list.ShowCompleted,
		"Translations":  i18n.GetAllLocales(),
		"Locales":       i18n.AvailableLocales(),
		"DefaultLang":   i18n.GetDefaultLang(),
	})
}

// GetLists returns all lists (JSON API)
func GetLists(c *fiber.Ctx) error {
	lists, err := db.GetAllLists()
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}

	// Check if JSON format is requested
	if c.Query("format") == "json" {
		return c.JSON(lists)
	}

	// For HTML, redirect to homepage
	return c.Redirect("/")
}

// CreateList creates a new shopping list
func CreateList(c *fiber.Ctx) error {
	name := c.FormValue("name")
	if name == "" {
		return sendError(c, 400, "error.name_required")
	}
	if len(name) > MaxListNameLength {
		return sendError(c, 400, "error.name_too_long")
	}
	if name == "[HISTORY]" {
		return sendError(c, 400, "common.reserved_name")
	}

	// Check for duplicate name
	exists, err := db.ListNameExists(name, 0)
	if err != nil {
		return sendError(c, 500, "error.check_failed")
	}
	if exists {
		return sendError(c, 409, "list.name_exists")
	}

	icon := c.FormValue("icon")
	if icon == "" {
		icon = "🛒"
	}
	if len(icon) > MaxIconLength {
		return sendError(c, 400, "error.icon_too_long")
	}

	list, err := db.CreateList(name, icon)
	if err != nil {
		return sendError(c, 500, "error.create_failed")
	}

	// Broadcast to WebSocket clients
	BroadcastUpdate("list_created", list)

	// Return the new list item partial for HTMX
	return c.Render("partials/list_item", fiber.Map{
		"List": list,
	}, "")
}

// UpdateList updates a list's name and icon
func UpdateList(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	name := c.FormValue("name")
	if name == "" {
		return sendError(c, 400, "error.name_required")
	}
	if len(name) > MaxListNameLength {
		return sendError(c, 400, "error.name_too_long")
	}
	if name == "[HISTORY]" {
		return sendError(c, 400, "common.reserved_name")
	}

	// Check for duplicate name (excluding current list)
	exists, err := db.ListNameExists(name, id)
	if err != nil {
		return sendError(c, 500, "error.check_failed")
	}
	if exists {
		return sendError(c, 409, "list.name_exists")
	}

	icon := c.FormValue("icon")
	if len(icon) > MaxIconLength {
		return sendError(c, 400, "error.icon_too_long")
	}

	list, err := db.UpdateList(id, name, icon)
	if err != nil {
		return sendError(c, 500, "error.update_failed")
	}

	// Broadcast to WebSocket clients
	BroadcastUpdate("list_updated", list)

	// Return updated list item partial
	return c.Render("partials/list_item", fiber.Map{
		"List": list,
	}, "")
}

// DeleteList deletes a shopping list
func DeleteList(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	err = db.DeleteList(id)
	if err != nil {
		return c.Status(400).SendString(err.Error())
	}

	// Broadcast to WebSocket clients
	BroadcastUpdate("list_deleted", map[string]int64{"id": id})

	// Return empty string (HTMX will remove the element)
	return c.SendString("")
}

// SetActiveList sets a list as active
func SetActiveList(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	err = db.SetActiveList(id)
	if err != nil {
		return sendError(c, 500, "error.check_failed")
	}

	// Broadcast to WebSocket clients
	BroadcastUpdate("list_activated", map[string]int64{"id": id})

	// Check if this is an AJAX request (HTMX or fetch)
	isAjax := c.Get("HX-Request") != "" || c.Get("X-Requested-With") != ""
	if !isAjax {
		return c.Redirect(fmt.Sprintf("/lists/%d", id))
	}

	// Check if this is from the lists management page or main page
	currentURL := c.Get("HX-Current-URL")
	referer := c.Get("Referer")
	isListsPage := strings.Contains(currentURL, "/lists") || strings.Contains(referer, "/lists")

	if !isListsPage {
		c.Set("HX-Redirect", fmt.Sprintf("/lists/%d", id))
		return c.SendString("")
	}

	// Return updated lists for the management page
	return returnAllLists(c)
}

// MoveListUp moves a list up in order
func MoveListUp(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	err = db.MoveListUp(id)
	if err != nil {
		return sendError(c, 500, "error.move_failed")
	}

	BroadcastUpdate("lists_reordered", nil)
	return c.SendStatus(200)
}

// MoveListDown moves a list down in order
func MoveListDown(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	err = db.MoveListDown(id)
	if err != nil {
		return sendError(c, 500, "error.move_failed")
	}

	BroadcastUpdate("lists_reordered", nil)
	return c.SendStatus(200)
}

// Helper to return all lists as HTML partials
func returnAllLists(c *fiber.Ctx) error {
	lists, err := db.GetAllLists()
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}

	activeList, _ := db.GetActiveList()

	return c.Render("partials/lists_container", fiber.Map{
		"Lists":      lists,
		"ActiveList": activeList,
	}, "")
}

// ToggleShowCompleted toggles the show_completed setting for a list
func ToggleShowCompleted(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).SendString("Invalid ID")
	}

	list, err := db.ToggleListShowCompleted(id)
	if err != nil {
		return c.Status(500).SendString("Failed to toggle show completed")
	}

	// Broadcast to WebSocket clients
	BroadcastUpdate("list_updated", list)

	// Return the updated sections list
	sections, err := db.GetSectionsByList(id)
	if err != nil {
		return c.Status(500).SendString("Failed to fetch sections")
	}

	return c.Render("partials/sections_list", fiber.Map{
		"Sections":      sections,
		"ShowCompleted": list.ShowCompleted,
	}, "")
}

// sectionRenderMap builds the template data map for rendering a single section partial
func sectionRenderMap(section *db.Section) fiber.Map {
	return fiber.Map{
		"Section":       section,
		"Sections":      getSectionsForDropdown(),
		"ShowCompleted": db.GetShowCompletedForSection(section.ID),
	}
}

// UploadListCoverImage attaches a cover image to a shopping list.
// Mirrors handlers.UploadRecipeCoverImage exactly, swapping recipe-id for list-id.
func UploadListCoverImage(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if _, err := db.GetListByID(id); err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.not_found")
		}
		log.Printf("[UPLOAD] fetch list %d failed: %v", id, err)
		return sendError(c, 500, "error.fetch_failed")
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
			log.Printf("[UPLOAD] list cover save failed: %v", err)
			return sendError(c, 500, "error.image_save_failed")
		}
	}

	// Capture the previous path BEFORE overwriting so we can clean it up.
	oldPath, _ := db.GetListCoverImage(id)

	if err := db.SetListCoverImage(id, &filename); err != nil {
		log.Printf("[UPLOAD] set list cover for %d failed: %v", id, err)
		return sendError(c, 500, "error.image_save_failed")
	}

	if oldPath != "" && oldPath != filename && UploadsRoot() != "" {
		fullPath := filepath.Join(UploadsRoot(), oldPath)
		if rmErr := os.Remove(fullPath); rmErr != nil && !os.IsNotExist(rmErr) {
			log.Printf("[UPLOAD] remove old list cover %q failed: %v", fullPath, rmErr)
		}
	}

	url := "/uploads/" + filename
	BroadcastUpdate("list_cover_updated", map[string]interface{}{
		"list_id":         id,
		"cover_image_url": url,
	})

	return c.JSON(fiber.Map{
		"list_id":         id,
		"cover_image_url": url,
	})
}

// DeleteListCoverImage clears a list's cover image and removes the file from disk.
func DeleteListCoverImage(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if _, err := db.GetListByID(id); err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.not_found")
		}
		log.Printf("[UPLOAD] fetch list %d failed: %v", id, err)
		return sendError(c, 500, "error.fetch_failed")
	}

	oldPath, _ := db.GetListCoverImage(id)

	if err := db.SetListCoverImage(id, nil); err != nil {
		log.Printf("[UPLOAD] clear list cover for %d failed: %v", id, err)
		return sendError(c, 500, "error.image_save_failed")
	}

	if oldPath != "" && UploadsRoot() != "" {
		fullPath := filepath.Join(UploadsRoot(), oldPath)
		if rmErr := os.Remove(fullPath); rmErr != nil && !os.IsNotExist(rmErr) {
			log.Printf("[UPLOAD] remove list cover %q failed: %v", fullPath, rmErr)
		}
	}

	BroadcastUpdate("list_cover_updated", map[string]interface{}{
		"list_id":         id,
		"cover_image_url": "",
	})

	return c.SendStatus(204)
}
