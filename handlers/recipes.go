package handlers

import (
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"shopping-list/db"
	"shopping-list/i18n"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// recipeListItem is a lightweight recipe payload used by GetRecipes that adds
// IngredientCount to the wire shape. We embed db.Recipe so existing JSON keys
// stay identical and clients get the new key for free.
type recipeListItem struct {
	db.Recipe
	IngredientCount int `json:"ingredient_count"`
}

// Input length limits for recipes (mirrors handlers/lists.go:16-22 style).
const (
	MaxRecipeNameLength        = 100
	MaxRecipeDescriptionLength = 500
	MaxIngredientNameLength    = 100
	MaxIngredientNotesLength   = 200
	MaxStepContentLength       = 1000
)

// validUnits is the closed set of unit strings accepted by ingredient handlers.
// Two semantic groups (cooking vs packaging) plus the special to_taste — see
// db.MeasurementUnits / db.PackageUnits for the apply-to-list dispatch.
var validUnits = map[string]bool{
	// Cooking / measurement
	"tsp": true, "tbsp": true, "cup": true, "fl_oz": true, "oz": true,
	"lb": true, "g": true, "kg": true, "ml": true, "l": true,
	// Packaging / discrete
	"whole": true, "can": true, "jar": true, "bottle": true, "package": true,
	"bunch": true, "head": true, "dozen": true, "slice": true, "loaf": true,
	"clove": true,
	// Special
	"to_taste": true,
}

func isValidUnit(u string) bool {
	return validUnits[u]
}

// IsValidUnit is the exported version of isValidUnit, used by the REST API
// package so the unit allowlist has a single source of truth.
func IsValidUnit(u string) bool {
	return isValidUnit(u)
}

// formatUnitForDescription formats a unit for human-readable item.description text.
// Used when seeding new shopping-list items from a recipe.
func formatUnitForDescription(unit string) string {
	switch unit {
	case "to_taste":
		return "to taste"
	case "fl_oz":
		return "fl oz"
	default:
		return unit
	}
}

// GetRecipesPage was a stub in Phase 6. Recipes now live as a section on the
// home page, so /recipes simply redirects there. Keeps any old bookmarks alive.
func GetRecipesPage(c *fiber.Ctx) error {
	return c.Redirect("/")
}

// GetRecipes returns all recipes as JSON, decorated with ingredient_count so
// the home-page recipe cards can show "N ingredients" without a per-recipe
// follow-up fetch. Per-recipe ingredient lookup is N+1 against db.GetRecipeIngredients
// — acceptable here because recipe counts stay small (single-digit users).
func GetRecipes(c *fiber.Ctx) error {
	recipes, err := db.GetRecipes()
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}
	if recipes == nil {
		return c.JSON([]recipeListItem{})
	}

	out := make([]recipeListItem, 0, len(recipes))
	for _, r := range recipes {
		count := 0
		if ings, ierr := db.GetRecipeIngredients(r.ID); ierr == nil {
			count = len(ings)
		}
		out = append(out, recipeListItem{Recipe: r, IngredientCount: count})
	}
	return c.JSON(out)
}

// GetRecipe returns one recipe. Default response is HTML (the detail page).
// JSON is returned when the request asks for it via Accept header or ?format=json.
// Mirrors the dual-purpose pattern used by GetLists at handlers/lists.go.
func GetRecipe(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		if wantsJSON(c) {
			return sendError(c, 400, "error.invalid_id")
		}
		return c.Redirect("/")
	}

	recipe, err := db.GetRecipe(id)
	if err != nil {
		if err == sql.ErrNoRows {
			if wantsJSON(c) {
				return sendError(c, 404, "error.recipe_not_found")
			}
			return c.Redirect("/")
		}
		return sendError(c, 500, "error.fetch_failed")
	}

	if wantsJSON(c) {
		return c.JSON(recipe)
	}

	lists, _ := db.GetAllLists()

	return c.Render("recipe", fiber.Map{
		"Recipe":       recipe,
		"Lists":        lists,
		"Translations": i18n.GetAllLocales(),
		"Locales":      i18n.AvailableLocales(),
		"DefaultLang":  i18n.GetDefaultLang(),
	})
}

// wantsJSON returns true when the client asked for JSON via Accept: application/json
// or ?format=json query param. Mirrors the pattern in handlers/lists.go GetLists.
func wantsJSON(c *fiber.Ctx) bool {
	if c.Query("format") == "json" {
		return true
	}
	if accept := c.Get("Accept"); accept != "" && strings.Contains(strings.ToLower(accept), "application/json") {
		return true
	}
	return false
}

// CreateRecipe creates a new recipe.
func CreateRecipe(c *fiber.Ctx) error {
	name := c.FormValue("name")
	if name == "" {
		return sendError(c, 400, "error.recipe_name_required")
	}
	if len(name) > MaxRecipeNameLength {
		return sendError(c, 400, "error.recipe_name_too_long")
	}
	description := c.FormValue("description")
	if len(description) > MaxRecipeDescriptionLength {
		return sendError(c, 400, "error.name_too_long")
	}

	recipe, err := db.CreateRecipe(name, description)
	if err != nil {
		return sendError(c, 500, "error.create_failed")
	}

	BroadcastUpdate("recipe_created", recipe)
	return c.JSON(recipe)
}

// UpdateRecipe updates a recipe's name and description.
func UpdateRecipe(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	name := c.FormValue("name")
	if name == "" {
		return sendError(c, 400, "error.recipe_name_required")
	}
	if len(name) > MaxRecipeNameLength {
		return sendError(c, 400, "error.recipe_name_too_long")
	}
	description := c.FormValue("description")
	if len(description) > MaxRecipeDescriptionLength {
		return sendError(c, 400, "error.name_too_long")
	}

	if err := db.UpdateRecipe(id, name, description); err != nil {
		return sendError(c, 500, "error.update_failed")
	}

	recipe, err := db.GetRecipe(id)
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}

	BroadcastUpdate("recipe_updated", recipe)
	return c.JSON(recipe)
}

// DeleteRecipe deletes a recipe; FK CASCADE removes ingredients and steps.
func DeleteRecipe(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if err := db.DeleteRecipe(id); err != nil {
		return sendError(c, 500, "error.delete_failed")
	}

	BroadcastUpdate("recipe_deleted", map[string]int64{"id": id})
	return c.SendStatus(200)
}

// MoveRecipeUp moves a recipe up in sort order.
func MoveRecipeUp(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}
	if err := db.MoveRecipeUp(id); err != nil {
		return sendError(c, 500, "error.move_failed")
	}
	BroadcastUpdate("recipe_moved", map[string]int64{"id": id})
	return c.SendStatus(200)
}

// MoveRecipeDown moves a recipe down in sort order.
func MoveRecipeDown(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}
	if err := db.MoveRecipeDown(id); err != nil {
		return sendError(c, 500, "error.move_failed")
	}
	BroadcastUpdate("recipe_moved", map[string]int64{"id": id})
	return c.SendStatus(200)
}

// parseIngredientForm extracts (name, *quantity, unit, notes) from form values.
// Rules:
//   - unit must be in validUnits.
//   - unit "to_taste" → quantity forced to NULL (lenient).
//   - any other unit  → quantity must parse as float > 0.
//   - notes optional, max MaxIngredientNotesLength chars.
func parseIngredientForm(c *fiber.Ctx) (name string, quantity *float64, unit, notes string, errKey string) {
	name = strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return "", nil, "", "", "error.ingredient_name_required"
	}
	if len(name) > MaxIngredientNameLength {
		return "", nil, "", "", "error.name_too_long"
	}

	unit = c.FormValue("unit")
	if !isValidUnit(unit) {
		return "", nil, "", "", "error.invalid_unit"
	}

	notes = strings.TrimSpace(c.FormValue("notes"))
	if len(notes) > MaxIngredientNotesLength {
		return "", nil, "", "", "error.name_too_long"
	}

	rawQty := strings.TrimSpace(c.FormValue("quantity"))

	if unit == "to_taste" {
		// Lenient: ignore any provided quantity; store NULL.
		return name, nil, unit, notes, ""
	}

	if rawQty == "" {
		return "", nil, "", "", "error.ingredient_quantity_required"
	}
	q, parseErr := strconv.ParseFloat(rawQty, 64)
	if parseErr != nil || q <= 0 {
		return "", nil, "", "", "error.ingredient_quantity_invalid"
	}
	return name, &q, unit, notes, ""
}

// AddRecipeIngredient adds an ingredient to a recipe.
func AddRecipeIngredient(c *fiber.Ctx) error {
	recipeID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if _, err := db.GetRecipe(recipeID); err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.recipe_not_found")
		}
		return sendError(c, 500, "error.fetch_failed")
	}

	name, quantity, unit, notes, errKey := parseIngredientForm(c)
	if errKey != "" {
		return sendError(c, 400, errKey)
	}

	ingredient, err := db.AddRecipeIngredient(recipeID, name, quantity, unit, notes)
	if err != nil {
		return sendError(c, 500, "error.create_failed")
	}

	BroadcastUpdate("recipe_ingredient_created", ingredient)
	return c.JSON(ingredient)
}

// UpdateRecipeIngredient updates an ingredient's name, quantity, unit, and notes.
func UpdateRecipeIngredient(c *fiber.Ctx) error {
	if _, err := strconv.ParseInt(c.Params("id"), 10, 64); err != nil {
		return sendError(c, 400, "error.invalid_id")
	}
	ingredientID, err := strconv.ParseInt(c.Params("ingredientId"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if _, err := db.GetRecipeIngredient(ingredientID); err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.ingredient_not_found")
		}
		return sendError(c, 500, "error.fetch_failed")
	}

	name, quantity, unit, notes, errKey := parseIngredientForm(c)
	if errKey != "" {
		return sendError(c, 400, errKey)
	}

	if err := db.UpdateRecipeIngredient(ingredientID, name, quantity, unit, notes); err != nil {
		return sendError(c, 500, "error.update_failed")
	}

	updated, err := db.GetRecipeIngredient(ingredientID)
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}

	BroadcastUpdate("recipe_ingredient_updated", updated)
	return c.JSON(updated)
}

// DeleteRecipeIngredient deletes an ingredient.
func DeleteRecipeIngredient(c *fiber.Ctx) error {
	if _, err := strconv.ParseInt(c.Params("id"), 10, 64); err != nil {
		return sendError(c, 400, "error.invalid_id")
	}
	ingredientID, err := strconv.ParseInt(c.Params("ingredientId"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if err := db.DeleteRecipeIngredient(ingredientID); err != nil {
		return sendError(c, 500, "error.delete_failed")
	}

	BroadcastUpdate("recipe_ingredient_deleted", map[string]int64{"id": ingredientID})
	return c.SendStatus(200)
}

// formValuesByName collects every value submitted under `name` (and `name[]`)
// across multipart/form-data and url-encoded bodies. Order is preserved.
//
// Why we need this: Fiber's c.FormValue returns only the FIRST occurrence, and
// c.MultipartForm() errors out for urlencoded bodies. For repeated form keys
// (`ordered_ids[]=1&ordered_ids[]=2&...`), we have to drop down to fasthttp's
// PostArgs.PeekMulti.
func formValuesByName(c *fiber.Ctx, name string) []string {
	var out []string

	// Multipart bodies first.
	if form, err := c.MultipartForm(); err == nil && form != nil {
		if v, ok := form.Value[name+"[]"]; ok {
			out = append(out, v...)
		}
		if v, ok := form.Value[name]; ok {
			out = append(out, v...)
		}
	}

	// Urlencoded body — PeekMulti returns every value for the key.
	args := c.Request().PostArgs()
	for _, b := range args.PeekMulti(name + "[]") {
		out = append(out, string(b))
	}
	for _, b := range args.PeekMulti(name) {
		out = append(out, string(b))
	}

	return out
}

// parseIDList parses a slice of form values as int64 IDs, skipping empties.
func parseIDList(values []string) ([]int64, error) {
	ids := make([]int64, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, err
		}
		ids = append(ids, n)
	}
	return ids, nil
}

// parseOrderedIDs reads `ordered_ids[]` (form-encoded, repeated) into []int64.
func parseOrderedIDs(c *fiber.Ctx) ([]int64, error) {
	return parseIDList(formValuesByName(c, "ordered_ids"))
}

// ReorderRecipeIngredients reorders ingredients by sort_order using ordered_ids[].
func ReorderRecipeIngredients(c *fiber.Ctx) error {
	recipeID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	ids, err := parseOrderedIDs(c)
	if err != nil || len(ids) == 0 {
		return sendError(c, 400, "error.no_ids")
	}

	if err := db.ReorderRecipeIngredients(recipeID, ids); err != nil {
		return sendError(c, 500, "error.reorder_failed")
	}

	BroadcastUpdate("recipe_ingredients_reordered", map[string]int64{"recipe_id": recipeID})
	return c.SendStatus(200)
}

// AddRecipeStep appends a step to a recipe.
func AddRecipeStep(c *fiber.Ctx) error {
	recipeID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if _, err := db.GetRecipe(recipeID); err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.recipe_not_found")
		}
		return sendError(c, 500, "error.fetch_failed")
	}

	content := strings.TrimSpace(c.FormValue("content"))
	if content == "" {
		return sendError(c, 400, "error.step_content_required")
	}
	if len(content) > MaxStepContentLength {
		return sendError(c, 400, "error.name_too_long")
	}

	step, err := db.AddRecipeStep(recipeID, content)
	if err != nil {
		return sendError(c, 500, "error.create_failed")
	}

	BroadcastUpdate("recipe_step_created", step)
	return c.JSON(step)
}

// UpdateRecipeStep updates a step's content.
func UpdateRecipeStep(c *fiber.Ctx) error {
	if _, err := strconv.ParseInt(c.Params("id"), 10, 64); err != nil {
		return sendError(c, 400, "error.invalid_id")
	}
	stepID, err := strconv.ParseInt(c.Params("stepId"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if _, err := db.GetRecipeStep(stepID); err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.step_not_found")
		}
		return sendError(c, 500, "error.fetch_failed")
	}

	content := strings.TrimSpace(c.FormValue("content"))
	if content == "" {
		return sendError(c, 400, "error.step_content_required")
	}
	if len(content) > MaxStepContentLength {
		return sendError(c, 400, "error.name_too_long")
	}

	if err := db.UpdateRecipeStep(stepID, content); err != nil {
		return sendError(c, 500, "error.update_failed")
	}

	updated, err := db.GetRecipeStep(stepID)
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}

	BroadcastUpdate("recipe_step_updated", updated)
	return c.JSON(updated)
}

// DeleteRecipeStep deletes a step (also renumbers remaining steps).
func DeleteRecipeStep(c *fiber.Ctx) error {
	if _, err := strconv.ParseInt(c.Params("id"), 10, 64); err != nil {
		return sendError(c, 400, "error.invalid_id")
	}
	stepID, err := strconv.ParseInt(c.Params("stepId"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if err := db.DeleteRecipeStep(stepID); err != nil {
		return sendError(c, 500, "error.delete_failed")
	}

	BroadcastUpdate("recipe_step_deleted", map[string]int64{"id": stepID})
	return c.SendStatus(200)
}

// ReorderRecipeSteps assigns step_number based on the position of each ID
// in the ordered_ids[] list.
func ReorderRecipeSteps(c *fiber.Ctx) error {
	recipeID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	ids, err := parseOrderedIDs(c)
	if err != nil || len(ids) == 0 {
		return sendError(c, 400, "error.no_ids")
	}

	if err := db.ReorderRecipeSteps(recipeID, ids); err != nil {
		return sendError(c, 500, "error.reorder_failed")
	}

	BroadcastUpdate("recipe_steps_reordered", map[string]int64{"recipe_id": recipeID})
	return c.SendStatus(200)
}

// ToggleRecipeStepCompleted flips the per-step completion flag.
// Returns the updated step JSON. Broadcasts recipe_step_completed_changed so
// open recipe tabs sync without a full reload.
func ToggleRecipeStepCompleted(c *fiber.Ctx) error {
	recipeID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}
	stepID, err := strconv.ParseInt(c.Params("stepId"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	step, err := db.GetRecipeStep(stepID)
	if err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.step_not_found")
		}
		return sendError(c, 500, "error.fetch_failed")
	}
	if step.RecipeID != recipeID {
		return sendError(c, 404, "error.step_not_found")
	}

	newState, err := db.ToggleRecipeStepCompleted(stepID)
	if err != nil {
		return sendError(c, 500, "error.toggle_failed")
	}

	updated, err := db.GetRecipeStep(stepID)
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}

	BroadcastUpdate("recipe_step_completed_changed", map[string]interface{}{
		"recipe_id": recipeID,
		"step_id":   stepID,
		"completed": newState,
	})

	return c.JSON(updated)
}

// ResetRecipeStepsCompleted clears completion on every step of a recipe.
// 204 No Content on success. Broadcasts recipe_steps_reset so open tabs sync.
func ResetRecipeStepsCompleted(c *fiber.Ctx) error {
	recipeID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if _, err := db.GetRecipe(recipeID); err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.recipe_not_found")
		}
		return sendError(c, 500, "error.fetch_failed")
	}

	if err := db.ResetRecipeStepsCompleted(recipeID); err != nil {
		return sendError(c, 500, "error.update_failed")
	}

	BroadcastUpdate("recipe_steps_reset", map[string]int64{"recipe_id": recipeID})
	return c.SendStatus(204)
}

// ApplyRecipe is the centerpiece: bulk-create items in a target list (existing or new)
// from the recipe's ingredients.
//
// Form params:
//   - target           required: "existing" or "new"
//   - list_id          required if target=existing
//   - list_name        required if target=new
//   - ingredient_ids[] required, at least one
//
// On success returns:
//   - HTMX: 200 with HX-Redirect: /lists/<targetListID>
//   - API:  200 JSON {"list_id": <id>}
func ApplyRecipe(c *fiber.Ctx) error {
	recipeID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	if _, err := db.GetRecipe(recipeID); err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.recipe_not_found")
		}
		return sendError(c, 500, "error.fetch_failed")
	}

	target := c.FormValue("target")
	if target != "existing" && target != "new" {
		return sendError(c, 400, "error.invalid_apply_target")
	}

	ingredientIDs, err := parseIngredientIDs(c)
	if err != nil {
		return sendError(c, 400, "error.no_ingredients_selected")
	}
	if len(ingredientIDs) == 0 {
		return sendError(c, 400, "error.no_ingredients_selected")
	}

	var targetListID int64
	if target == "existing" {
		listIDStr := c.FormValue("list_id")
		if listIDStr == "" {
			return sendError(c, 400, "error.invalid_list_id")
		}
		targetListID, err = strconv.ParseInt(listIDStr, 10, 64)
		if err != nil {
			return sendError(c, 400, "error.invalid_list_id")
		}
		if _, err := db.GetListByID(targetListID); err != nil {
			return sendError(c, 404, "error.not_found")
		}
	} else {
		listName := strings.TrimSpace(c.FormValue("list_name"))
		if listName == "" {
			return sendError(c, 400, "error.list_name_required")
		}
		if len(listName) > MaxListNameLength {
			return sendError(c, 400, "error.name_too_long")
		}
		newList, err := db.CreateList(listName, "")
		if err != nil {
			return sendError(c, 500, "error.create_failed")
		}
		targetListID = newList.ID
		BroadcastUpdate("list_created", newList)
	}

	if err := db.ApplyRecipeToList(recipeID, targetListID, ingredientIDs); err != nil {
		return sendError(c, 500, "error.apply_failed")
	}

	BroadcastUpdate("recipe_applied", map[string]int64{
		"recipe_id": recipeID,
		"list_id":   targetListID,
	})

	// HTMX gets a redirect header so the client lands on the destination list.
	// Plain API callers (no HX-Request) get JSON.
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/lists/"+strconv.FormatInt(targetListID, 10))
		return c.SendStatus(200)
	}
	return c.JSON(fiber.Map{"list_id": targetListID})
}

// parseIngredientIDs reads `ingredient_ids[]` (form-encoded, repeated) into []int64.
func parseIngredientIDs(c *fiber.Ctx) ([]int64, error) {
	return parseIDList(formValuesByName(c, "ingredient_ids"))
}

// UploadRecipeCoverImage attaches a cover image to a recipe. Reuses the
// existing item-image upload pipeline (saveUploadedImage, sentinel errors,
// MaxImageSize, HEIC re-encoding) — recipe covers are stored on the same
// disk and served by the same /uploads/ filesystem mount, but the path is
// kept on recipes.cover_image_path rather than item_history.image_path.
func UploadRecipeCoverImage(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	recipe, err := db.GetRecipe(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.recipe_not_found")
		}
		log.Printf("[UPLOAD] fetch recipe %d failed: %v", id, err)
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
			log.Printf("[UPLOAD] recipe cover save failed: %v", err)
			return sendError(c, 500, "error.image_save_failed")
		}
	}

	// Capture the previous path BEFORE overwriting so we can clean it up after
	// a successful DB update. Skipped when the new file is the same hash as the
	// old one (re-uploading the same image hits sha256 dedup → identical filename).
	var oldPath string
	if recipe.CoverImagePath != nil {
		oldPath = *recipe.CoverImagePath
	}

	if err := db.SetRecipeCoverImage(id, &filename); err != nil {
		log.Printf("[UPLOAD] set recipe cover for %d failed: %v", id, err)
		return sendError(c, 500, "error.image_save_failed")
	}

	if oldPath != "" && oldPath != filename && UploadsRoot() != "" {
		fullPath := filepath.Join(UploadsRoot(), oldPath)
		if rmErr := os.Remove(fullPath); rmErr != nil && !os.IsNotExist(rmErr) {
			log.Printf("[UPLOAD] remove old recipe cover %q failed: %v", fullPath, rmErr)
		}
	}

	url := "/uploads/" + filename
	BroadcastUpdate("recipe_cover_updated", map[string]interface{}{
		"recipe_id":       id,
		"cover_image_url": url,
	})

	return c.JSON(fiber.Map{
		"recipe_id":       id,
		"cover_image_url": url,
	})
}

// DeleteRecipeCoverImage clears a recipe's cover image and removes the file
// from disk. The DB is the source of truth — file removal errors are logged
// but do not fail the request.
func DeleteRecipeCoverImage(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	recipe, err := db.GetRecipe(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.recipe_not_found")
		}
		log.Printf("[UPLOAD] fetch recipe %d failed: %v", id, err)
		return sendError(c, 500, "error.fetch_failed")
	}

	var oldPath string
	if recipe.CoverImagePath != nil {
		oldPath = *recipe.CoverImagePath
	}

	if err := db.SetRecipeCoverImage(id, nil); err != nil {
		log.Printf("[UPLOAD] clear recipe cover for %d failed: %v", id, err)
		return sendError(c, 500, "error.image_save_failed")
	}

	if oldPath != "" && UploadsRoot() != "" {
		fullPath := filepath.Join(UploadsRoot(), oldPath)
		if rmErr := os.Remove(fullPath); rmErr != nil && !os.IsNotExist(rmErr) {
			log.Printf("[UPLOAD] remove recipe cover %q failed: %v", fullPath, rmErr)
		}
	}

	BroadcastUpdate("recipe_cover_updated", map[string]interface{}{
		"recipe_id":       id,
		"cover_image_url": "",
	})

	return c.SendStatus(204)
}
