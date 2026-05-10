package api

import (
	"shopping-list/handlers"

	"github.com/gofiber/fiber/v2"
)

// UploadImageByName / DeleteImageByName are thin pass-throughs to the handlers
// package. Same multipart-form-file upload shape works for both HTMX and REST
// callers; both responses are already JSON / 204.

func UploadImageByName(c *fiber.Ctx) error {
	return handlers.UploadImageByName(c)
}

func DeleteImageByName(c *fiber.Ctx) error {
	return handlers.DeleteImageByName(c)
}
