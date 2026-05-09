package handlers

import (
	"database/sql"
	"shopping-list/db"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Input length limits for recipes (mirrors handlers/lists.go:16-22 style).
const (
	MaxRecipeNameLength        = 100
	MaxRecipeDescriptionLength = 500
	MaxIngredientNameLength    = 100
	MaxStepContentLength       = 1000
)

// validUnits is the closed set of unit strings accepted by ingredient handlers.
// Stored as plain strings on recipe_ingredients.unit; validated in Go (mirrors
// section.sort_mode validation at db/queries.go:UpdateSectionSortMode).
var validUnits = map[string]bool{
	"tsp":      true,
	"tbsp":     true,
	"cup":      true,
	"fl_oz":    true,
	"oz":       true,
	"lb":       true,
	"g":        true,
	"kg":       true,
	"ml":       true,
	"l":        true,
	"whole":    true,
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

// GetRecipesPage renders the recipes list page. Stub for now — the real template
// arrives in Phase 7. Returning a minimal HTML string so the route is curlable.
func GetRecipesPage(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(`<!doctype html><meta charset="utf-8"><title>Recipes</title><body style="font-family:sans-serif;padding:2rem;color:#444"><h1>Recipes</h1><p>UI coming in Phase 7. Use <code>/recipes/list</code> (or <code>/api/v1/recipes</code> with <code>Authorization: Bearer &lt;token&gt;</code>) for the JSON API.</p></body>`)
}

// GetRecipes returns all recipes as JSON (no ingredients/steps).
func GetRecipes(c *fiber.Ctx) error {
	recipes, err := db.GetRecipes()
	if err != nil {
		return sendError(c, 500, "error.fetch_failed")
	}
	if recipes == nil {
		recipes = []db.Recipe{}
	}
	return c.JSON(recipes)
}

// GetRecipe returns one recipe with ingredients + steps as JSON.
func GetRecipe(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return sendError(c, 400, "error.invalid_id")
	}

	recipe, err := db.GetRecipe(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return sendError(c, 404, "error.recipe_not_found")
		}
		return sendError(c, 500, "error.fetch_failed")
	}
	return c.JSON(recipe)
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

// parseIngredientForm extracts (name, *quantity, unit) from form values, applying
// the unit/quantity rules:
//   - unit must be in validUnits.
//   - unit "to_taste"  → quantity is forced to NULL regardless of input (lenient).
//   - any other unit   → quantity must be present and > 0.
func parseIngredientForm(c *fiber.Ctx) (name string, quantity *int, unit string, errKey string) {
	name = strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return "", nil, "", "error.ingredient_name_required"
	}
	if len(name) > MaxIngredientNameLength {
		return "", nil, "", "error.name_too_long"
	}

	unit = c.FormValue("unit")
	if !isValidUnit(unit) {
		return "", nil, "", "error.invalid_unit"
	}

	rawQty := strings.TrimSpace(c.FormValue("quantity"))

	if unit == "to_taste" {
		// Lenient: ignore any provided quantity; store NULL.
		return name, nil, unit, ""
	}

	if rawQty == "" {
		return "", nil, "", "error.ingredient_quantity_required"
	}
	q, parseErr := strconv.Atoi(rawQty)
	if parseErr != nil || q <= 0 {
		return "", nil, "", "error.ingredient_quantity_invalid"
	}
	return name, &q, unit, ""
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

	name, quantity, unit, errKey := parseIngredientForm(c)
	if errKey != "" {
		return sendError(c, 400, errKey)
	}

	ingredient, err := db.AddRecipeIngredient(recipeID, name, quantity, unit)
	if err != nil {
		return sendError(c, 500, "error.create_failed")
	}

	BroadcastUpdate("recipe_ingredient_created", ingredient)
	return c.JSON(ingredient)
}

// UpdateRecipeIngredient updates an ingredient's name, quantity, and unit.
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

	name, quantity, unit, errKey := parseIngredientForm(c)
	if errKey != "" {
		return sendError(c, 400, errKey)
	}

	if err := db.UpdateRecipeIngredient(ingredientID, name, quantity, unit); err != nil {
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
