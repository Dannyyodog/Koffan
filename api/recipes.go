package api

import (
	"database/sql"
	"shopping-list/db"
	"shopping-list/handlers"

	"github.com/gofiber/fiber/v2"
)

// CreateRecipeRequest is the JSON body for POST/PUT /api/v1/recipes.
type CreateRecipeRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CreateIngredientRequest is the JSON body for POST/PUT /api/v1/recipes/:id/ingredients.
// Quantity is *int so callers can omit it for "to taste".
type CreateIngredientRequest struct {
	Name     string `json:"name"`
	Quantity *int   `json:"quantity,omitempty"`
	Unit     string `json:"unit"`
}

// CreateStepRequest is the JSON body for POST/PUT /api/v1/recipes/:id/steps.
type CreateStepRequest struct {
	Content string `json:"content"`
}

// ApplyRecipeRequest is the JSON body for POST /api/v1/recipes/:id/apply.
type ApplyRecipeRequest struct {
	Target        string  `json:"target"` // "existing" or "new"
	ListID        int64   `json:"list_id,omitempty"`
	ListName      string  `json:"list_name,omitempty"`
	IngredientIDs []int64 `json:"ingredient_ids"`
}

// errResp is a tiny helper for consistent JSON error bodies.
func errResp(c *fiber.Ctx, status int, code, msg string) error {
	return c.Status(status).JSON(ErrorResponse{Error: code, Message: msg})
}

// GetRecipes returns all recipes (no nested ingredients/steps).
func GetRecipes(c *fiber.Ctx) error {
	recipes, err := db.GetRecipes()
	if err != nil {
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch recipes")
	}
	if recipes == nil {
		recipes = []db.Recipe{}
	}
	return c.JSON(fiber.Map{"recipes": recipes})
}

// GetRecipe returns one recipe with ingredients + steps.
func GetRecipe(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	r, err := db.GetRecipe(int64(id))
	if err != nil {
		if err == sql.ErrNoRows {
			return errResp(c, fiber.StatusNotFound, "not_found", "Recipe not found")
		}
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch recipe")
	}
	return c.JSON(r)
}

// CreateRecipe creates a new recipe.
func CreateRecipe(c *fiber.Ctx) error {
	var req CreateRecipeRequest
	if err := c.BodyParser(&req); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_json", "Failed to parse request body")
	}
	if req.Name == "" {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Name is required")
	}
	if len(req.Name) > handlers.MaxRecipeNameLength {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Name is too long")
	}
	if len(req.Description) > handlers.MaxRecipeDescriptionLength {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Description is too long")
	}
	r, err := db.CreateRecipe(req.Name, req.Description)
	if err != nil {
		return errResp(c, fiber.StatusInternalServerError, "create_failed", "Failed to create recipe")
	}
	handlers.BroadcastUpdate("recipe_created", r)
	return c.Status(fiber.StatusCreated).JSON(r)
}

// UpdateRecipe updates a recipe's name/description.
func UpdateRecipe(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	existing, err := db.GetRecipe(int64(id))
	if err != nil {
		if err == sql.ErrNoRows {
			return errResp(c, fiber.StatusNotFound, "not_found", "Recipe not found")
		}
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch recipe")
	}
	var req CreateRecipeRequest
	if err := c.BodyParser(&req); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_json", "Failed to parse request body")
	}
	name := req.Name
	if name == "" {
		name = existing.Name
	}
	desc := req.Description
	if len(name) > handlers.MaxRecipeNameLength {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Name is too long")
	}
	if len(desc) > handlers.MaxRecipeDescriptionLength {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Description is too long")
	}
	if err := db.UpdateRecipe(int64(id), name, desc); err != nil {
		return errResp(c, fiber.StatusInternalServerError, "update_failed", "Failed to update recipe")
	}
	updated, _ := db.GetRecipe(int64(id))
	handlers.BroadcastUpdate("recipe_updated", updated)
	return c.JSON(updated)
}

// DeleteRecipe deletes a recipe; FK CASCADE removes ingredients and steps.
func DeleteRecipe(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	if _, err := db.GetRecipe(int64(id)); err != nil {
		if err == sql.ErrNoRows {
			return errResp(c, fiber.StatusNotFound, "not_found", "Recipe not found")
		}
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch recipe")
	}
	if err := db.DeleteRecipe(int64(id)); err != nil {
		return errResp(c, fiber.StatusInternalServerError, "delete_failed", "Failed to delete recipe")
	}
	handlers.BroadcastUpdate("recipe_deleted", map[string]int64{"id": int64(id)})
	return c.SendStatus(fiber.StatusNoContent)
}

// validateIngredientReq applies the same unit/quantity rules used by the HTMX handler.
// Returns (cleanedQuantityPointer, errResponseOrNil).
func validateIngredientReq(c *fiber.Ctx, req *CreateIngredientRequest) (*int, error) {
	if req.Name == "" {
		return nil, errResp(c, fiber.StatusBadRequest, "validation_error", "Ingredient name is required")
	}
	if len(req.Name) > handlers.MaxIngredientNameLength {
		return nil, errResp(c, fiber.StatusBadRequest, "validation_error", "Ingredient name is too long")
	}
	if !handlers.IsValidUnit(req.Unit) {
		return nil, errResp(c, fiber.StatusBadRequest, "validation_error", "Invalid unit")
	}
	if req.Unit == "to_taste" {
		// Lenient: ignore any quantity, store NULL.
		return nil, nil
	}
	if req.Quantity == nil {
		return nil, errResp(c, fiber.StatusBadRequest, "validation_error", "Ingredient quantity is required")
	}
	if *req.Quantity <= 0 {
		return nil, errResp(c, fiber.StatusBadRequest, "validation_error", "Ingredient quantity must be a positive number")
	}
	return req.Quantity, nil
}

// AddRecipeIngredient adds an ingredient to a recipe.
func AddRecipeIngredient(c *fiber.Ctx) error {
	recipeID, err := c.ParamsInt("id")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	if _, err := db.GetRecipe(int64(recipeID)); err != nil {
		if err == sql.ErrNoRows {
			return errResp(c, fiber.StatusNotFound, "not_found", "Recipe not found")
		}
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch recipe")
	}
	var req CreateIngredientRequest
	if err := c.BodyParser(&req); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_json", "Failed to parse request body")
	}
	qty, errResponse := validateIngredientReq(c, &req)
	if errResponse != nil {
		return errResponse
	}
	ing, err := db.AddRecipeIngredient(int64(recipeID), req.Name, qty, req.Unit)
	if err != nil {
		return errResp(c, fiber.StatusInternalServerError, "create_failed", "Failed to create ingredient")
	}
	handlers.BroadcastUpdate("recipe_ingredient_created", ing)
	return c.Status(fiber.StatusCreated).JSON(ing)
}

// UpdateRecipeIngredient updates an ingredient in place.
func UpdateRecipeIngredient(c *fiber.Ctx) error {
	if _, err := c.ParamsInt("id"); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	ingredientID, err := c.ParamsInt("ingredientId")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid ingredient ID")
	}
	if _, err := db.GetRecipeIngredient(int64(ingredientID)); err != nil {
		if err == sql.ErrNoRows {
			return errResp(c, fiber.StatusNotFound, "not_found", "Ingredient not found")
		}
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch ingredient")
	}
	var req CreateIngredientRequest
	if err := c.BodyParser(&req); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_json", "Failed to parse request body")
	}
	qty, errResponse := validateIngredientReq(c, &req)
	if errResponse != nil {
		return errResponse
	}
	if err := db.UpdateRecipeIngredient(int64(ingredientID), req.Name, qty, req.Unit); err != nil {
		return errResp(c, fiber.StatusInternalServerError, "update_failed", "Failed to update ingredient")
	}
	updated, _ := db.GetRecipeIngredient(int64(ingredientID))
	handlers.BroadcastUpdate("recipe_ingredient_updated", updated)
	return c.JSON(updated)
}

// DeleteRecipeIngredient deletes an ingredient.
func DeleteRecipeIngredient(c *fiber.Ctx) error {
	if _, err := c.ParamsInt("id"); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	ingredientID, err := c.ParamsInt("ingredientId")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid ingredient ID")
	}
	if err := db.DeleteRecipeIngredient(int64(ingredientID)); err != nil {
		return errResp(c, fiber.StatusInternalServerError, "delete_failed", "Failed to delete ingredient")
	}
	handlers.BroadcastUpdate("recipe_ingredient_deleted", map[string]int64{"id": int64(ingredientID)})
	return c.SendStatus(fiber.StatusNoContent)
}

// AddRecipeStep appends a step to a recipe.
func AddRecipeStep(c *fiber.Ctx) error {
	recipeID, err := c.ParamsInt("id")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	if _, err := db.GetRecipe(int64(recipeID)); err != nil {
		if err == sql.ErrNoRows {
			return errResp(c, fiber.StatusNotFound, "not_found", "Recipe not found")
		}
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch recipe")
	}
	var req CreateStepRequest
	if err := c.BodyParser(&req); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_json", "Failed to parse request body")
	}
	if req.Content == "" {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Step content is required")
	}
	if len(req.Content) > handlers.MaxStepContentLength {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Step content is too long")
	}
	s, err := db.AddRecipeStep(int64(recipeID), req.Content)
	if err != nil {
		return errResp(c, fiber.StatusInternalServerError, "create_failed", "Failed to create step")
	}
	handlers.BroadcastUpdate("recipe_step_created", s)
	return c.Status(fiber.StatusCreated).JSON(s)
}

// UpdateRecipeStep updates a step's content.
func UpdateRecipeStep(c *fiber.Ctx) error {
	if _, err := c.ParamsInt("id"); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	stepID, err := c.ParamsInt("stepId")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid step ID")
	}
	if _, err := db.GetRecipeStep(int64(stepID)); err != nil {
		if err == sql.ErrNoRows {
			return errResp(c, fiber.StatusNotFound, "not_found", "Step not found")
		}
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch step")
	}
	var req CreateStepRequest
	if err := c.BodyParser(&req); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_json", "Failed to parse request body")
	}
	if req.Content == "" {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Step content is required")
	}
	if len(req.Content) > handlers.MaxStepContentLength {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Step content is too long")
	}
	if err := db.UpdateRecipeStep(int64(stepID), req.Content); err != nil {
		return errResp(c, fiber.StatusInternalServerError, "update_failed", "Failed to update step")
	}
	updated, _ := db.GetRecipeStep(int64(stepID))
	handlers.BroadcastUpdate("recipe_step_updated", updated)
	return c.JSON(updated)
}

// DeleteRecipeStep deletes a step (also renumbers remaining steps).
func DeleteRecipeStep(c *fiber.Ctx) error {
	if _, err := c.ParamsInt("id"); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	stepID, err := c.ParamsInt("stepId")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid step ID")
	}
	if err := db.DeleteRecipeStep(int64(stepID)); err != nil {
		return errResp(c, fiber.StatusInternalServerError, "delete_failed", "Failed to delete step")
	}
	handlers.BroadcastUpdate("recipe_step_deleted", map[string]int64{"id": int64(stepID)})
	return c.SendStatus(fiber.StatusNoContent)
}

// ApplyRecipe is the REST mirror of the HTMX handler.
// Returns 200 + {"list_id": <id>} on success (no HX-Redirect for API callers).
func ApplyRecipe(c *fiber.Ctx) error {
	recipeID, err := c.ParamsInt("id")
	if err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_id", "Invalid recipe ID")
	}
	if _, err := db.GetRecipe(int64(recipeID)); err != nil {
		if err == sql.ErrNoRows {
			return errResp(c, fiber.StatusNotFound, "not_found", "Recipe not found")
		}
		return errResp(c, fiber.StatusInternalServerError, "db_error", "Failed to fetch recipe")
	}
	var req ApplyRecipeRequest
	if err := c.BodyParser(&req); err != nil {
		return errResp(c, fiber.StatusBadRequest, "invalid_json", "Failed to parse request body")
	}
	if req.Target != "existing" && req.Target != "new" {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "target must be 'existing' or 'new'")
	}
	if len(req.IngredientIDs) == 0 {
		return errResp(c, fiber.StatusBadRequest, "validation_error", "Select at least one ingredient")
	}

	var targetListID int64
	if req.Target == "existing" {
		if req.ListID <= 0 {
			return errResp(c, fiber.StatusBadRequest, "validation_error", "list_id is required when target=existing")
		}
		if _, err := db.GetListByID(req.ListID); err != nil {
			return errResp(c, fiber.StatusNotFound, "not_found", "Target list not found")
		}
		targetListID = req.ListID
	} else {
		if req.ListName == "" {
			return errResp(c, fiber.StatusBadRequest, "validation_error", "list_name is required when target=new")
		}
		newList, err := db.CreateList(req.ListName, "")
		if err != nil {
			return errResp(c, fiber.StatusInternalServerError, "create_failed", "Failed to create list")
		}
		targetListID = newList.ID
		handlers.BroadcastUpdate("list_created", newList)
	}

	if err := db.ApplyRecipeToList(int64(recipeID), targetListID, req.IngredientIDs); err != nil {
		return errResp(c, fiber.StatusInternalServerError, "apply_failed", "Failed to apply recipe")
	}

	handlers.BroadcastUpdate("recipe_applied", map[string]int64{
		"recipe_id": int64(recipeID),
		"list_id":   targetListID,
	})

	return c.JSON(fiber.Map{"list_id": targetListID})
}

// UploadRecipeCoverImage and DeleteRecipeCoverImage are thin pass-throughs to
// the handlers package — the upload/delete shape (multipart form file upload
// for POST, no body for DELETE) is identical for HTMX and REST API callers,
// and both responses are already JSON. Wrapping keeps api/api.go consistent
// with the local-symbol convention used by every other v1 route.

func UploadRecipeCoverImage(c *fiber.Ctx) error {
	return handlers.UploadRecipeCoverImage(c)
}

func DeleteRecipeCoverImage(c *fiber.Ctx) error {
	return handlers.DeleteRecipeCoverImage(c)
}
