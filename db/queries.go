package db

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Section represents a shopping list section
type Section struct {
	ID        int64     `json:"id"`
	ListID    int64     `json:"list_id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sort_order"`
	SortMode  string    `json:"sort_mode"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt int64     `json:"updated_at"`
	Items     []Item    `json:"items"`
}

// Item represents a shopping list item
type Item struct {
	ID          int64     `json:"id"`
	SectionID   int64     `json:"section_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Completed   bool      `json:"completed"`
	Uncertain   bool      `json:"uncertain"`
	Quantity    int       `json:"quantity"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   int64     `json:"updated_at"`
	ImagePath   string    `json:"image_path"`
}

// Session represents a user session
type Session struct {
	ID        string
	ExpiresAt int64
}

// List represents a shopping list
type List struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Icon           string    `json:"icon"`
	SortOrder      int       `json:"sort_order"`
	IsActive       bool      `json:"is_active"`
	ShowCompleted  bool      `json:"show_completed"`
	CoverImagePath *string   `json:"cover_image_path,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      int64     `json:"updated_at"`
	Stats          Stats     `json:"stats,omitempty"`
}

// Template represents a reusable template
type Template struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	SortOrder   int            `json:"sort_order"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   int64          `json:"updated_at"`
	Items       []TemplateItem `json:"items,omitempty"`
}

// TemplateItem represents an item in a template
type TemplateItem struct {
	ID          int64     `json:"id"`
	TemplateID  int64     `json:"template_id"`
	SectionName string    `json:"section_name"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
}

// ==================== LISTS ====================

// listSelectWithStats is a shared SELECT that joins lists with aggregated item stats in a single query.
const listSelectWithStats = `
	SELECT l.id, l.name, COALESCE(l.icon, '🛒'), l.sort_order, l.is_active,
	       COALESCE(l.show_completed, TRUE), l.cover_image_path, l.created_at, COALESCE(l.updated_at, 0),
	       COALESCE(COUNT(i.id), 0) AS total_items,
	       COALESCE(SUM(CASE WHEN i.completed = TRUE THEN 1 ELSE 0 END), 0) AS completed_items
	FROM lists l
	LEFT JOIN sections s ON s.list_id = l.id
	LEFT JOIN items i ON i.section_id = s.id
`

// scanListWithStats scans one row produced by listSelectWithStats and populates stats.
func scanListWithStats(scanner interface {
	Scan(dest ...interface{}) error
}) (List, error) {
	var l List
	var total, completed int
	var cover sql.NullString
	if err := scanner.Scan(&l.ID, &l.Name, &l.Icon, &l.SortOrder, &l.IsActive, &l.ShowCompleted, &cover, &l.CreatedAt, &l.UpdatedAt, &total, &completed); err != nil {
		return l, err
	}
	if cover.Valid {
		s := cover.String
		l.CoverImagePath = &s
	}
	l.Stats.TotalItems = total
	l.Stats.CompletedItems = completed
	if total > 0 {
		l.Stats.Percentage = (completed * 100) / total
	}
	return l, nil
}

// GetAllLists returns all shopping lists with their stats (single query with GROUP BY).
func GetAllLists() ([]List, error) {
	rows, err := DB.Query(listSelectWithStats + `
		GROUP BY l.id
		ORDER BY l.sort_order ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []List
	for rows.Next() {
		l, err := scanListWithStats(rows)
		if err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}
	return lists, nil
}

// GetListByID returns a single list by ID with stats.
func GetListByID(id int64) (*List, error) {
	row := DB.QueryRow(listSelectWithStats+`
		WHERE l.id = ?
		GROUP BY l.id
	`, id)
	l, err := scanListWithStats(row)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// GetActiveList returns the currently active list with stats.
func GetActiveList() (*List, error) {
	row := DB.QueryRow(listSelectWithStats + `
		WHERE l.is_active = TRUE
		GROUP BY l.id
		LIMIT 1
	`)
	l, err := scanListWithStats(row)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// CreateList creates a new shopping list
func CreateList(name, icon string) (*List, error) {
	var maxOrder int
	DB.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM lists").Scan(&maxOrder)

	if icon == "" {
		icon = "🛒"
	}

	result, err := DB.Exec(`
		INSERT INTO lists (name, icon, sort_order, is_active) VALUES (?, ?, ?, FALSE)
	`, name, icon, maxOrder+1)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return GetListByID(id)
}

// ListNameExists checks if a list with the given name already exists (case-insensitive)
// excludeID allows excluding a specific list (useful when updating)
func ListNameExists(name string, excludeID int64) (bool, error) {
	var count int
	var err error
	if excludeID > 0 {
		err = DB.QueryRow(`
			SELECT COUNT(*) FROM lists
			WHERE name = ? COLLATE NOCASE AND id != ?
		`, name, excludeID).Scan(&count)
	} else {
		err = DB.QueryRow(`
			SELECT COUNT(*) FROM lists
			WHERE name = ? COLLATE NOCASE
		`, name).Scan(&count)
	}
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateList updates a list's name and icon
func UpdateList(id int64, name, icon string) (*List, error) {
	if icon == "" {
		_, err := DB.Exec(`UPDATE lists SET name = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, name, id)
		if err != nil {
			return nil, err
		}
	} else {
		_, err := DB.Exec(`UPDATE lists SET name = ?, icon = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, name, icon, id)
		if err != nil {
			return nil, err
		}
	}
	return GetListByID(id)
}

// ToggleListShowCompleted toggles the show_completed flag on a list
func ToggleListShowCompleted(id int64) (*List, error) {
	_, err := DB.Exec(`UPDATE lists SET show_completed = NOT COALESCE(show_completed, TRUE), updated_at = strftime('%s', 'now') WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	return GetListByID(id)
}

// SetListShowCompleted explicitly sets the show_completed flag on a list
func SetListShowCompleted(id int64, value bool) (*List, error) {
	_, err := DB.Exec(`UPDATE lists SET show_completed = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, value, id)
	if err != nil {
		return nil, err
	}
	return GetListByID(id)
}

// GetShowCompletedForSection returns the show_completed setting for the list a section belongs to
func GetShowCompletedForSection(sectionID int64) bool {
	var showCompleted bool
	err := DB.QueryRow(`SELECT COALESCE(l.show_completed, TRUE) FROM lists l JOIN sections s ON s.list_id = l.id WHERE s.id = ?`, sectionID).Scan(&showCompleted)
	if err != nil {
		return true
	}
	return showCompleted
}

// DeleteList deletes a list and all its sections/items
func DeleteList(id int64) error {
	_, err := DB.Exec(`DELETE FROM lists WHERE id = ?`, id)
	return err
}

// SetActiveList sets a list as the active one
func SetActiveList(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Deactivate all lists
	_, err = tx.Exec("UPDATE lists SET is_active = FALSE")
	if err != nil {
		return err
	}

	// Activate the selected list
	_, err = tx.Exec("UPDATE lists SET is_active = TRUE, updated_at = strftime('%s', 'now') WHERE id = ?", id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// MoveListUp moves a list up in sort order
func MoveListUp(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentOrder int
	err = tx.QueryRow("SELECT sort_order FROM lists WHERE id = ?", id).Scan(&currentOrder)
	if err != nil {
		return err
	}

	if currentOrder == 0 {
		return nil
	}

	_, err = tx.Exec(`UPDATE lists SET sort_order = sort_order + 1 WHERE sort_order = ?`, currentOrder-1)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`UPDATE lists SET sort_order = ? WHERE id = ?`, currentOrder-1, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// MoveListDown moves a list down in sort order
func MoveListDown(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentOrder, maxOrder int
	err = tx.QueryRow("SELECT sort_order FROM lists WHERE id = ?", id).Scan(&currentOrder)
	if err != nil {
		return err
	}
	err = tx.QueryRow("SELECT MAX(sort_order) FROM lists").Scan(&maxOrder)
	if err != nil {
		return err
	}

	if currentOrder >= maxOrder {
		return nil
	}

	_, err = tx.Exec(`UPDATE lists SET sort_order = sort_order - 1 WHERE sort_order = ?`, currentOrder+1)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`UPDATE lists SET sort_order = ? WHERE id = ?`, currentOrder+1, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetListStats returns stats for a specific list using a single aggregated query.
func GetListStats(listID int64) Stats {
	var stats Stats
	DB.QueryRow(`
		SELECT COUNT(i.id), COALESCE(SUM(CASE WHEN i.completed = TRUE THEN 1 ELSE 0 END), 0)
		FROM items i
		JOIN sections s ON i.section_id = s.id
		WHERE s.list_id = ?
	`, listID).Scan(&stats.TotalItems, &stats.CompletedItems)
	if stats.TotalItems > 0 {
		stats.Percentage = (stats.CompletedItems * 100) / stats.TotalItems
	}
	return stats
}

// ==================== SECTIONS ====================

func GetAllSections() ([]Section, error) {
	activeList, err := GetActiveList()
	if err != nil {
		// Fallback: return all sections if no active list (shouldn't happen)
		return getAllSectionsGlobal()
	}
	return GetSectionsByList(activeList.ID)
}

// scanSectionRows reads all section rows into memory, then closes the result set.
// Must be called before any nested queries (required because MaxOpenConns=1).
func scanSectionRows(rows *sql.Rows) ([]Section, error) {
	defer rows.Close()
	var sections []Section
	for rows.Next() {
		var s Section
		if err := rows.Scan(&s.ID, &s.ListID, &s.Name, &s.SortOrder, &s.SortMode, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sections = append(sections, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sections, nil
}

// GetSectionsByList returns all sections for a specific list along with their items.
func GetSectionsByList(listID int64) ([]Section, error) {
	rows, err := DB.Query(`
		SELECT id, list_id, name, sort_order, COALESCE(sort_mode, 'manual'), created_at, COALESCE(updated_at, 0)
		FROM sections
		WHERE list_id = ?
		ORDER BY sort_order ASC
	`, listID)
	if err != nil {
		return nil, err
	}

	sections, err := scanSectionRows(rows)
	if err != nil {
		return nil, err
	}

	// Nested queries run AFTER the outer Rows are closed (MaxOpenConns=1 requires this).
	for i := range sections {
		sections[i].Items, err = getItemsBySectionWithMode(sections[i].ID, sections[i].SortMode)
		if err != nil {
			return nil, err
		}
	}
	return sections, nil
}

// getAllSectionsGlobal returns all sections (fallback, used during migration).
func getAllSectionsGlobal() ([]Section, error) {
	rows, err := DB.Query(`
		SELECT id, list_id, name, sort_order, COALESCE(sort_mode, 'manual'), created_at, COALESCE(updated_at, 0)
		FROM sections
		ORDER BY sort_order ASC
	`)
	if err != nil {
		return nil, err
	}

	sections, err := scanSectionRows(rows)
	if err != nil {
		return nil, err
	}

	for i := range sections {
		sections[i].Items, err = getItemsBySectionWithMode(sections[i].ID, sections[i].SortMode)
		if err != nil {
			return nil, err
		}
	}
	return sections, nil
}

func GetSectionByID(id int64) (*Section, error) {
	var s Section
	err := DB.QueryRow(`
		SELECT id, list_id, name, sort_order, COALESCE(sort_mode, 'manual'), created_at, COALESCE(updated_at, 0)
		FROM sections WHERE id = ?
	`, id).Scan(&s.ID, &s.ListID, &s.Name, &s.SortOrder, &s.SortMode, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.Items, err = getItemsBySectionWithMode(s.ID, s.SortMode)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func CreateSection(name string) (*Section, error) {
	activeList, err := GetActiveList()
	if err != nil {
		return nil, fmt.Errorf("no active list found")
	}
	return CreateSectionForList(activeList.ID, name)
}

// CreateSectionForList creates a section for a specific list
func CreateSectionForList(listID int64, name string) (*Section, error) {
	// Get max sort_order for this list
	var maxOrder int
	DB.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM sections WHERE list_id = ?", listID).Scan(&maxOrder)

	result, err := DB.Exec(`
		INSERT INTO sections (name, sort_order, list_id) VALUES (?, ?, ?)
	`, name, maxOrder+1, listID)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return GetSectionByID(id)
}

func UpdateSection(id int64, name string) (*Section, error) {
	_, err := DB.Exec(`UPDATE sections SET name = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, name, id)
	if err != nil {
		return nil, err
	}
	return GetSectionByID(id)
}

// UpdateSectionSortMode updates the sort mode for a section
func UpdateSectionSortMode(id int64, sortMode string) (*Section, error) {
	// Validate sort mode
	if sortMode != "manual" && sortMode != "alphabetical" && sortMode != "alphabetical_desc" {
		return nil, fmt.Errorf("invalid sort mode: %s", sortMode)
	}
	_, err := DB.Exec(`UPDATE sections SET sort_mode = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, sortMode, id)
	if err != nil {
		return nil, err
	}
	return GetSectionByID(id)
}

func DeleteSection(id int64) error {
	_, err := DB.Exec(`DELETE FROM sections WHERE id = ?`, id)
	return err
}

func MoveSectionUp(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentOrder int
	var listID int64
	err = tx.QueryRow("SELECT sort_order, list_id FROM sections WHERE id = ?", id).Scan(&currentOrder, &listID)
	if err != nil {
		return err
	}

	if currentOrder == 0 {
		return nil // Already at top
	}

	// Swap with previous section (within the same list)
	_, err = tx.Exec(`
		UPDATE sections SET sort_order = sort_order + 1
		WHERE sort_order = ? AND list_id = ?
	`, currentOrder-1, listID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE sections SET sort_order = ? WHERE id = ?
	`, currentOrder-1, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func MoveSectionDown(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentOrder int
	var listID int64
	err = tx.QueryRow("SELECT sort_order, list_id FROM sections WHERE id = ?", id).Scan(&currentOrder, &listID)
	if err != nil {
		return err
	}

	var maxOrder int
	err = tx.QueryRow("SELECT MAX(sort_order) FROM sections WHERE list_id = ?", listID).Scan(&maxOrder)
	if err != nil {
		return err
	}

	if currentOrder >= maxOrder {
		return nil // Already at bottom
	}

	// Swap with next section (within the same list)
	_, err = tx.Exec(`
		UPDATE sections SET sort_order = sort_order - 1
		WHERE sort_order = ? AND list_id = ?
	`, currentOrder+1, listID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE sections SET sort_order = ? WHERE id = ?
	`, currentOrder+1, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ==================== ITEMS ====================

// FindItemByNameInSection finds an existing item by name in a section (case-insensitive)
func FindItemByNameInSection(sectionID int64, name string) (*Item, error) {
	var i Item
	err := DB.QueryRow(`
		SELECT i.id, i.section_id, i.name, i.description, i.completed, i.uncertain, COALESCE(i.quantity, 0), i.sort_order, i.created_at, COALESCE(i.updated_at, 0), COALESCE(ih.image_path, '') AS image_path
		FROM items i
		LEFT JOIN item_history ih ON i.name = ih.name COLLATE NOCASE
		WHERE i.section_id = ? AND LOWER(i.name) = LOWER(?)
		LIMIT 1
	`, sectionID, name).Scan(&i.ID, &i.SectionID, &i.Name, &i.Description, &i.Completed, &i.Uncertain, &i.Quantity, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt, &i.ImagePath)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

// ReactivateItem unchecks a completed item and optionally updates description/quantity
func ReactivateItem(id int64, description string, quantity int) (*Item, error) {
	_, err := DB.Exec(`
		UPDATE items SET completed = FALSE, description = ?, quantity = ?, updated_at = strftime('%s', 'now')
		WHERE id = ?
	`, description, quantity, id)
	if err != nil {
		return nil, err
	}
	return GetItemByID(id)
}

func GetItemsBySection(sectionID int64) ([]Item, error) {
	// Look up sort_mode and delegate. Prefer getItemsBySectionWithMode when sort_mode is already known.
	var sortMode string
	if err := DB.QueryRow("SELECT COALESCE(sort_mode, 'manual') FROM sections WHERE id = ?", sectionID).Scan(&sortMode); err != nil {
		sortMode = "manual"
	}
	return getItemsBySectionWithMode(sectionID, sortMode)
}

// getItemsBySectionWithMode returns items for a section using an already-known sort_mode,
// avoiding the extra QueryRow that GetItemsBySection performs.
func getItemsBySectionWithMode(sectionID int64, sortMode string) ([]Item, error) {
	var query string
	switch sortMode {
	case "alphabetical":
		query = `
		SELECT i.id, i.section_id, i.name, i.description, i.completed, i.uncertain, COALESCE(i.quantity, 0), i.sort_order, i.created_at, COALESCE(i.updated_at, 0), COALESCE(ih.image_path, '') AS image_path
		FROM items i
		LEFT JOIN item_history ih ON i.name = ih.name COLLATE NOCASE
		WHERE i.section_id = ?
		ORDER BY i.completed ASC, i.name COLLATE NOCASE ASC`
	case "alphabetical_desc":
		query = `
		SELECT i.id, i.section_id, i.name, i.description, i.completed, i.uncertain, COALESCE(i.quantity, 0), i.sort_order, i.created_at, COALESCE(i.updated_at, 0), COALESCE(ih.image_path, '') AS image_path
		FROM items i
		LEFT JOIN item_history ih ON i.name = ih.name COLLATE NOCASE
		WHERE i.section_id = ?
		ORDER BY i.completed ASC, i.name COLLATE NOCASE DESC`
	default:
		query = `
		SELECT i.id, i.section_id, i.name, i.description, i.completed, i.uncertain, COALESCE(i.quantity, 0), i.sort_order, i.created_at, COALESCE(i.updated_at, 0), COALESCE(ih.image_path, '') AS image_path
		FROM items i
		LEFT JOIN item_history ih ON i.name = ih.name COLLATE NOCASE
		WHERE i.section_id = ?
		ORDER BY i.completed ASC, i.sort_order ASC`
	}

	rows, err := DB.Query(query, sectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var i Item
		err := rows.Scan(&i.ID, &i.SectionID, &i.Name, &i.Description, &i.Completed, &i.Uncertain, &i.Quantity, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt, &i.ImagePath)
		if err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, nil
}

// CheckAllItems marks all active items in a section as completed
func CheckAllItems(sectionID int64) (int64, error) {
	result, err := DB.Exec(`
		UPDATE items SET completed = TRUE, updated_at = strftime('%s', 'now')
		WHERE section_id = ? AND completed = FALSE
	`, sectionID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// UncheckAllItems marks all completed items in a section as active
func UncheckAllItems(sectionID int64) (int64, error) {
	result, err := DB.Exec(`
		UPDATE items SET completed = FALSE, updated_at = strftime('%s', 'now')
		WHERE section_id = ? AND completed = TRUE
	`, sectionID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func GetItemByID(id int64) (*Item, error) {
	var i Item
	err := DB.QueryRow(`
		SELECT i.id, i.section_id, i.name, i.description, i.completed, i.uncertain, COALESCE(i.quantity, 0), i.sort_order, i.created_at, COALESCE(i.updated_at, 0), COALESCE(ih.image_path, '') AS image_path
		FROM items i
		LEFT JOIN item_history ih ON i.name = ih.name COLLATE NOCASE
		WHERE i.id = ?
	`, id).Scan(&i.ID, &i.SectionID, &i.Name, &i.Description, &i.Completed, &i.Uncertain, &i.Quantity, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt, &i.ImagePath)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func CreateItem(sectionID int64, name, description string, quantity int) (*Item, error) {
	// Get max sort_order for this section
	var maxOrder int
	DB.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM items WHERE section_id = ?", sectionID).Scan(&maxOrder)

	result, err := DB.Exec(`
		INSERT INTO items (section_id, name, description, quantity, sort_order) VALUES (?, ?, ?, ?, ?)
	`, sectionID, name, description, quantity, maxOrder+1)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return GetItemByID(id)
}

func UpdateItem(id int64, name, description string, quantity int) (*Item, error) {
	_, err := DB.Exec(`
		UPDATE items SET name = ?, description = ?, quantity = ?, updated_at = strftime('%s', 'now') WHERE id = ?
	`, name, description, quantity, id)
	if err != nil {
		return nil, err
	}
	return GetItemByID(id)
}

// AdjustItemQuantity changes an item's quantity atomically.
// If absolute is non-nil, it sets quantity to MAX(0, *absolute).
// Otherwise, it adjusts by delta and clamps the result to MAX(0, current+delta).
// Returns the refreshed item.
func AdjustItemQuantity(id int64, delta int, absolute *int) (*Item, error) {
	var err error
	if absolute != nil {
		value := *absolute
		if value < 0 {
			value = 0
		}
		_, err = DB.Exec(`
			UPDATE items SET quantity = ?, updated_at = strftime('%s', 'now') WHERE id = ?
		`, value, id)
	} else {
		_, err = DB.Exec(`
			UPDATE items SET quantity = MAX(0, COALESCE(quantity, 0) + ?), updated_at = strftime('%s', 'now') WHERE id = ?
		`, delta, id)
	}
	if err != nil {
		return nil, err
	}
	return GetItemByID(id)
}

func DeleteItem(id int64) error {
	_, err := DB.Exec(`DELETE FROM items WHERE id = ?`, id)
	return err
}

// DeleteCompletedItems deletes all completed items from the active list
func DeleteCompletedItems() (int64, error) {
	activeList, err := GetActiveList()
	if err != nil {
		return 0, err
	}

	result, err := DB.Exec(`
		DELETE FROM items WHERE completed = TRUE AND section_id IN (
			SELECT id FROM sections WHERE list_id = ?
		)
	`, activeList.ID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func ToggleItemCompleted(id int64) (*Item, error) {
	_, err := DB.Exec(`UPDATE items SET completed = NOT completed, updated_at = strftime('%s', 'now') WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	return GetItemByID(id)
}

func ToggleItemUncertain(id int64) (*Item, error) {
	_, err := DB.Exec(`UPDATE items SET uncertain = NOT uncertain, updated_at = strftime('%s', 'now') WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	return GetItemByID(id)
}

func MoveItemToSection(id, newSectionID int64) (*Item, error) {
	// Get max sort_order in new section
	var maxOrder int
	DB.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM items WHERE section_id = ?", newSectionID).Scan(&maxOrder)

	_, err := DB.Exec(`
		UPDATE items SET section_id = ?, sort_order = ?, updated_at = strftime('%s', 'now') WHERE id = ?
	`, newSectionID, maxOrder+1, id)
	if err != nil {
		return nil, err
	}
	return GetItemByID(id)
}

// MoveItemToSectionAtPosition moves an item to a new section at a specific position among ACTIVE items
func MoveItemToSectionAtPosition(id, newSectionID int64, targetPosition int) (*Item, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Verify item exists and get current section
	var currentSectionID int64
	err = tx.QueryRow("SELECT section_id FROM items WHERE id = ?", id).Scan(&currentSectionID)
	if err != nil {
		return nil, err // Item not found
	}

	// If same section, use regular reorder logic instead
	if currentSectionID == newSectionID {
		tx.Rollback()
		return reorderItemInSection(id, targetPosition)
	}

	// Get all ACTIVE items in target section, ordered by sort_order
	rows, err := tx.Query(`
		SELECT id, sort_order FROM items
		WHERE section_id = ? AND completed = FALSE
		ORDER BY sort_order ASC
	`, newSectionID)
	if err != nil {
		return nil, err
	}

	type itemOrder struct {
		id        int64
		sortOrder int
	}
	var activeItems []itemOrder
	for rows.Next() {
		var item itemOrder
		if err := rows.Scan(&item.id, &item.sortOrder); err != nil {
			rows.Close()
			return nil, err
		}
		activeItems = append(activeItems, item)
	}
	rows.Close()

	// Determine the target sort_order
	var targetSortOrder int
	if len(activeItems) == 0 {
		// No active items - check if there are ANY items (completed) and use max+1
		var maxOrder int
		err = tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM items WHERE section_id = ?", newSectionID).Scan(&maxOrder)
		if err != nil {
			return nil, err
		}
		targetSortOrder = maxOrder + 1
	} else if targetPosition <= 0 {
		// Insert at beginning - use sort_order less than first
		targetSortOrder = activeItems[0].sortOrder
	} else if targetPosition >= len(activeItems) {
		// Insert at end - use sort_order greater than last active
		targetSortOrder = activeItems[len(activeItems)-1].sortOrder + 1
	} else {
		// Insert in middle - use sort_order of item at target position
		targetSortOrder = activeItems[targetPosition].sortOrder
	}

	// Shift all items with sort_order >= targetSortOrder up by 1
	_, err = tx.Exec(`
		UPDATE items SET sort_order = sort_order + 1
		WHERE section_id = ? AND sort_order >= ?
	`, newSectionID, targetSortOrder)
	if err != nil {
		return nil, err
	}

	// Move the item to new section with target sort_order
	_, err = tx.Exec(`
		UPDATE items SET section_id = ?, sort_order = ?, updated_at = strftime('%s', 'now')
		WHERE id = ?
	`, newSectionID, targetSortOrder, id)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return GetItemByID(id)
}

// reorderItemInSection moves an item to a specific position within its current section
func reorderItemInSection(id int64, targetPosition int) (*Item, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Get item's current section and sort_order
	var sectionID int64
	var currentSortOrder int
	err = tx.QueryRow("SELECT section_id, sort_order FROM items WHERE id = ?", id).Scan(&sectionID, &currentSortOrder)
	if err != nil {
		return nil, err
	}

	// Get all ACTIVE items in section (excluding the moved item), ordered by sort_order
	rows, err := tx.Query(`
		SELECT id, sort_order FROM items
		WHERE section_id = ? AND completed = FALSE AND id != ?
		ORDER BY sort_order ASC
	`, sectionID, id)
	if err != nil {
		return nil, err
	}

	type itemOrder struct {
		id        int64
		sortOrder int
	}
	var otherItems []itemOrder
	for rows.Next() {
		var item itemOrder
		if err := rows.Scan(&item.id, &item.sortOrder); err != nil {
			rows.Close()
			return nil, err
		}
		otherItems = append(otherItems, item)
	}
	rows.Close()

	// Clamp target position
	if targetPosition < 0 {
		targetPosition = 0
	}
	if targetPosition > len(otherItems) {
		targetPosition = len(otherItems)
	}

	// Renumber all active items with moved item at target position
	newOrder := 0
	for i, item := range otherItems {
		if i == targetPosition {
			// Insert moved item here
			_, err = tx.Exec("UPDATE items SET sort_order = ? WHERE id = ?", newOrder, id)
			if err != nil {
				return nil, err
			}
			newOrder++
		}
		_, err = tx.Exec("UPDATE items SET sort_order = ? WHERE id = ?", newOrder, item.id)
		if err != nil {
			return nil, err
		}
		newOrder++
	}

	// If target is at end
	if targetPosition >= len(otherItems) {
		_, err = tx.Exec("UPDATE items SET sort_order = ? WHERE id = ?", newOrder, id)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return GetItemByID(id)
}

func MoveItemUp(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var sectionID int64
	var sortOrder int
	err = tx.QueryRow("SELECT section_id, sort_order FROM items WHERE id = ?", id).Scan(&sectionID, &sortOrder)
	if err != nil {
		return err
	}

	// Find previous item (closest smaller sort_order) - handles non-contiguous sort_order
	var prevID int64
	var prevSortOrder int
	err = tx.QueryRow(`
		SELECT id, sort_order FROM items
		WHERE section_id = ? AND sort_order < ?
		ORDER BY sort_order DESC
		LIMIT 1
	`, sectionID, sortOrder).Scan(&prevID, &prevSortOrder)

	if err == sql.ErrNoRows {
		return nil // Already at top
	}
	if err != nil {
		return err
	}

	// Swap sort_order values
	_, err = tx.Exec("UPDATE items SET sort_order = ? WHERE id = ?", sortOrder, prevID)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE items SET sort_order = ? WHERE id = ?", prevSortOrder, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func MoveItemDown(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var sectionID int64
	var sortOrder int
	err = tx.QueryRow("SELECT section_id, sort_order FROM items WHERE id = ?", id).Scan(&sectionID, &sortOrder)
	if err != nil {
		return err
	}

	// Find next item (closest larger sort_order) - handles non-contiguous sort_order
	var nextID int64
	var nextSortOrder int
	err = tx.QueryRow(`
		SELECT id, sort_order FROM items
		WHERE section_id = ? AND sort_order > ?
		ORDER BY sort_order ASC
		LIMIT 1
	`, sectionID, sortOrder).Scan(&nextID, &nextSortOrder)

	if err == sql.ErrNoRows {
		return nil // Already at bottom
	}
	if err != nil {
		return err
	}

	// Swap sort_order values
	_, err = tx.Exec("UPDATE items SET sort_order = ? WHERE id = ?", sortOrder, nextID)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE items SET sort_order = ? WHERE id = ?", nextSortOrder, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ==================== SESSIONS ====================

func CreateSession(id string, expiresAt int64) error {
	_, err := DB.Exec(`INSERT INTO sessions (id, expires_at) VALUES (?, ?)`, id, expiresAt)
	return err
}

func GetSession(id string) (*Session, error) {
	var s Session
	err := DB.QueryRow(`SELECT id, expires_at FROM sessions WHERE id = ?`, id).Scan(&s.ID, &s.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func DeleteSession(id string) error {
	_, err := DB.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func CleanExpiredSessions() error {
	_, err := DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
	return err
}

// ==================== STATS ====================

type Stats struct {
	TotalItems     int `json:"total_items"`
	CompletedItems int `json:"completed_items"`
	Percentage     int `json:"percentage"`
}

func GetStats() Stats {
	activeList, err := GetActiveList()
	if err != nil {
		// Fallback to global stats
		return getGlobalStats()
	}
	return GetListStats(activeList.ID)
}

// getGlobalStats returns stats for all items (fallback) using a single aggregated query.
func getGlobalStats() Stats {
	var stats Stats
	DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN completed = TRUE THEN 1 ELSE 0 END), 0) FROM items`).Scan(&stats.TotalItems, &stats.CompletedItems)
	if stats.TotalItems > 0 {
		stats.Percentage = (stats.CompletedItems * 100) / stats.TotalItems
	}
	return stats
}

// ==================== SECTION STATS ====================

type SectionStats struct {
	TotalItems     int `json:"total_items"`
	CompletedItems int `json:"completed_items"`
	Percentage     int `json:"percentage"`
}

func GetSectionStats(sectionID int64) SectionStats {
	var stats SectionStats
	DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN completed = TRUE THEN 1 ELSE 0 END), 0)
		FROM items WHERE section_id = ?
	`, sectionID).Scan(&stats.TotalItems, &stats.CompletedItems)
	if stats.TotalItems > 0 {
		stats.Percentage = (stats.CompletedItems * 100) / stats.TotalItems
	}
	return stats
}

// ==================== BATCH DELETE SECTIONS ====================

func DeleteSections(ids []int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, id := range ids {
		_, err := tx.Exec("DELETE FROM sections WHERE id = ?", id)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ==================== ITEM HISTORY (Auto-completion) ====================

type ItemSuggestion struct {
	Name            string `json:"name"`
	LastSectionID   int64  `json:"last_section_id"`
	LastSectionName string `json:"last_section_name"`
	UsageCount      int    `json:"usage_count"`
}

// SaveItemHistory saves or updates item name in history for auto-completion
func SaveItemHistory(name string, sectionID int64) error {
	_, err := DB.Exec(`
		INSERT INTO item_history (name, last_section_id, usage_count, last_used_at)
		VALUES (?, ?, 1, strftime('%s', 'now'))
		ON CONFLICT(name COLLATE NOCASE) DO UPDATE SET
			last_section_id = excluded.last_section_id,
			usage_count = usage_count + 1,
			last_used_at = strftime('%s', 'now')
	`, name, sectionID)
	return err
}

// ==================== ITEM IMAGES ====================

// UpsertItemImage attaches an image filename to the given item name in item_history.
// Images are keyed by name (case-insensitive via the existing UNIQUE(name COLLATE NOCASE)
// constraint), so all items sharing a name share an image. If no history row exists
// for the name, a new one is inserted with last_section_id NULL and usage_count 0.
func UpsertItemImage(name, imagePath string) error {
	_, err := DB.Exec(`
		INSERT INTO item_history (name, image_path, last_section_id, usage_count, last_used_at)
		VALUES (?, ?, NULL, 0, strftime('%s', 'now'))
		ON CONFLICT(name COLLATE NOCASE) DO UPDATE SET
			image_path = excluded.image_path,
			last_used_at = strftime('%s', 'now')
	`, name, imagePath)
	return err
}

// DeleteItemImage clears the image_path for the given item name and returns the
// previous filename so the caller can remove the file from disk. Returns an empty
// string if no image was attached.
func DeleteItemImage(name string) (string, error) {
	var oldPath sql.NullString
	err := DB.QueryRow(`
		SELECT image_path FROM item_history WHERE name = ? COLLATE NOCASE LIMIT 1
	`, name).Scan(&oldPath)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}

	_, err = DB.Exec(`
		UPDATE item_history SET image_path = NULL, last_used_at = strftime('%s', 'now')
		WHERE name = ? COLLATE NOCASE
	`, name)
	if err != nil {
		return "", err
	}

	if oldPath.Valid {
		return oldPath.String, nil
	}
	return "", nil
}

// SaveItemHistoryWithCount saves item history with a specific usage count (used for import)
func SaveItemHistoryWithCount(name string, sectionID int64, usageCount int) error {
	_, err := DB.Exec(`
		INSERT INTO item_history (name, last_section_id, usage_count, last_used_at)
		VALUES (?, ?, ?, strftime('%s', 'now'))
		ON CONFLICT(name COLLATE NOCASE) DO UPDATE SET
			last_section_id = CASE WHEN excluded.last_section_id > 0 THEN excluded.last_section_id ELSE last_section_id END,
			usage_count = CASE WHEN excluded.usage_count > usage_count THEN excluded.usage_count ELSE usage_count END,
			last_used_at = strftime('%s', 'now')
	`, name, sectionID, usageCount)
	return err
}

// SaveItemHistoryWithCountTx saves item history with a specific usage count within a transaction
func SaveItemHistoryWithCountTx(tx *sql.Tx, name string, sectionID int64, usageCount int) error {
	_, err := tx.Exec(`
		INSERT INTO item_history (name, last_section_id, usage_count, last_used_at)
		VALUES (?, ?, ?, strftime('%s', 'now'))
		ON CONFLICT(name COLLATE NOCASE) DO UPDATE SET
			last_section_id = CASE WHEN excluded.last_section_id > 0 THEN excluded.last_section_id ELSE last_section_id END,
			usage_count = CASE WHEN excluded.usage_count > usage_count THEN excluded.usage_count ELSE usage_count END,
			last_used_at = strftime('%s', 'now')
	`, name, sectionID, usageCount)
	return err
}

// levenshteinDistance calculates the edit distance between two strings using
// a rolling two-row buffer: O(min(len(s1), len(s2))) memory instead of O(n*m).
func levenshteinDistance(s1, s2 string) int {
	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)

	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	// Ensure s2 is the shorter string so the rolling rows are as small as possible.
	if len(s1) < len(s2) {
		s1, s2 = s2, s1
	}

	prev := make([]int, len(s2)+1)
	curr := make([]int, len(s2)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(s1); i++ {
		curr[0] = i
		for j := 1; j <= len(s2); j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			curr[j] = min(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}

	return prev[len(s2)]
}

// scoreSuggestion calculates a match score (higher is better)
func scoreSuggestion(name, query string) int {
	nameLower := strings.ToLower(name)
	queryLower := strings.ToLower(query)

	// Exact match: highest score
	if nameLower == queryLower {
		return 1000
	}

	// Prefix match: high score
	if strings.HasPrefix(nameLower, queryLower) {
		return 500
	}

	// Contains match: medium score
	if strings.Contains(nameLower, queryLower) {
		return 200
	}

	// Fuzzy match: score based on Levenshtein distance
	// Only consider if query is at least 3 chars and distance is reasonable
	if len(query) >= 3 {
		distance := levenshteinDistance(nameLower, queryLower)
		maxDistance := len(query) / 2 // Allow ~50% typos

		if distance <= maxDistance {
			return 100 - distance*20 // Lower score for more typos
		}

		// Also check if any word in the name fuzzy matches
		words := strings.Fields(nameLower)
		for _, word := range words {
			wordDist := levenshteinDistance(word, queryLower)
			if wordDist <= maxDistance {
				return 80 - wordDist*15
			}
		}
	}

	return 0 // No match
}

// GetItemSuggestions returns item name suggestions matching the query with fuzzy matching
func GetItemSuggestions(query string, limit int) ([]ItemSuggestion, error) {
	if limit <= 0 {
		limit = 10
	}

	// Fetch more items to allow for fuzzy matching and scoring
	rows, err := DB.Query(`
		SELECT h.name, COALESCE(h.last_section_id, 0), COALESCE(s.name, ''), h.usage_count
		FROM item_history h
		LEFT JOIN sections s ON h.last_section_id = s.id
		ORDER BY h.usage_count DESC, h.last_used_at DESC
		LIMIT 200
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scoredSuggestion struct {
		suggestion ItemSuggestion
		score      int
	}

	var scored []scoredSuggestion
	for rows.Next() {
		var s ItemSuggestion
		if err := rows.Scan(&s.Name, &s.LastSectionID, &s.LastSectionName, &s.UsageCount); err != nil {
			return nil, err
		}

		score := scoreSuggestion(s.Name, query)
		if score > 0 {
			// Boost score slightly by usage count
			score += s.UsageCount / 10
			scored = append(scored, scoredSuggestion{s, score})
		}
	}

	// Sort by score (descending), then by usage_count (descending)
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].suggestion.UsageCount > scored[j].suggestion.UsageCount
	})

	// Return top results
	var suggestions []ItemSuggestion
	for i := 0; i < len(scored) && i < limit; i++ {
		suggestions = append(suggestions, scored[i].suggestion)
	}

	return suggestions, nil
}

// GetAllItemSuggestions returns all item suggestions for offline cache
func GetAllItemSuggestions(limit int) ([]ItemSuggestion, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := DB.Query(`
		SELECT h.name, COALESCE(h.last_section_id, 0), COALESCE(s.name, ''), h.usage_count
		FROM item_history h
		LEFT JOIN sections s ON h.last_section_id = s.id
		ORDER BY h.usage_count DESC, h.last_used_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []ItemSuggestion
	for rows.Next() {
		var s ItemSuggestion
		if err := rows.Scan(&s.Name, &s.LastSectionID, &s.LastSectionName, &s.UsageCount); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, s)
	}
	return suggestions, nil
}

// HistoryItem represents an item from history with ID for management
type HistoryItem struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	LastSectionID   int64  `json:"last_section_id"`
	LastSectionName string `json:"last_section_name"`
	UsageCount      int    `json:"usage_count"`
}

// GetItemHistoryList returns all history items for management UI
func GetItemHistoryList() ([]HistoryItem, error) {
	rows, err := DB.Query(`
		SELECT h.id, h.name, COALESCE(h.last_section_id, 0), COALESCE(s.name, ''), h.usage_count
		FROM item_history h
		LEFT JOIN sections s ON h.last_section_id = s.id
		ORDER BY h.usage_count DESC, h.last_used_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []HistoryItem
	for rows.Next() {
		var h HistoryItem
		if err := rows.Scan(&h.ID, &h.Name, &h.LastSectionID, &h.LastSectionName, &h.UsageCount); err != nil {
			return nil, err
		}
		items = append(items, h)
	}
	return items, nil
}

// DeleteItemHistory deletes a single item from history
func DeleteItemHistory(id int64) error {
	result, err := DB.Exec("DELETE FROM item_history WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("history item not found")
	}
	return nil
}

// DeleteItemHistoryBatch deletes multiple items from history
func DeleteItemHistoryBatch(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	// Build placeholders
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM item_history WHERE id IN (%s)", strings.Join(placeholders, ","))
	result, err := DB.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ==================== TEMPLATES ====================

// GetAllTemplates returns all templates with their items.
func GetAllTemplates() ([]Template, error) {
	rows, err := DB.Query(`
		SELECT id, name, description, sort_order, created_at, COALESCE(updated_at, 0)
		FROM templates
		ORDER BY sort_order ASC
	`)
	if err != nil {
		return nil, err
	}

	var templates []Template
	func() {
		defer rows.Close()
		for rows.Next() {
			var t Template
			if err = rows.Scan(&t.ID, &t.Name, &t.Description, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt); err != nil {
				return
			}
			templates = append(templates, t)
		}
		err = rows.Err()
	}()
	if err != nil {
		return nil, err
	}

	// Nested queries run after the outer result set is closed (MaxOpenConns=1).
	for i := range templates {
		templates[i].Items, err = GetTemplateItems(templates[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return templates, nil
}

// GetTemplateByID returns a single template by ID with items
func GetTemplateByID(id int64) (*Template, error) {
	var t Template
	err := DB.QueryRow(`
		SELECT id, name, description, sort_order, created_at, COALESCE(updated_at, 0)
		FROM templates WHERE id = ?
	`, id).Scan(&t.ID, &t.Name, &t.Description, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	t.Items, err = GetTemplateItems(t.ID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTemplateItems returns all items for a template
func GetTemplateItems(templateID int64) ([]TemplateItem, error) {
	rows, err := DB.Query(`
		SELECT id, template_id, section_name, name, description, sort_order, created_at
		FROM template_items
		WHERE template_id = ?
		ORDER BY section_name ASC, sort_order ASC
	`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TemplateItem
	for rows.Next() {
		var ti TemplateItem
		err := rows.Scan(&ti.ID, &ti.TemplateID, &ti.SectionName, &ti.Name, &ti.Description, &ti.SortOrder, &ti.CreatedAt)
		if err != nil {
			return nil, err
		}
		items = append(items, ti)
	}
	return items, nil
}

// CreateTemplate creates a new template
func CreateTemplate(name, description string) (*Template, error) {
	var maxOrder int
	DB.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM templates").Scan(&maxOrder)

	result, err := DB.Exec(`
		INSERT INTO templates (name, description, sort_order) VALUES (?, ?, ?)
	`, name, description, maxOrder+1)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return GetTemplateByID(id)
}

// UpdateTemplate updates a template's name and description
func UpdateTemplate(id int64, name, description string) (*Template, error) {
	_, err := DB.Exec(`
		UPDATE templates SET name = ?, description = ?, updated_at = strftime('%s', 'now') WHERE id = ?
	`, name, description, id)
	if err != nil {
		return nil, err
	}
	return GetTemplateByID(id)
}

// DeleteTemplate deletes a template and all its items
func DeleteTemplate(id int64) error {
	_, err := DB.Exec(`DELETE FROM templates WHERE id = ?`, id)
	return err
}

// AddTemplateItem adds an item to a template
func AddTemplateItem(templateID int64, sectionName, name, description string) (*TemplateItem, error) {
	var maxOrder int
	DB.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM template_items WHERE template_id = ?", templateID).Scan(&maxOrder)

	result, err := DB.Exec(`
		INSERT INTO template_items (template_id, section_name, name, description, sort_order)
		VALUES (?, ?, ?, ?, ?)
	`, templateID, sectionName, name, description, maxOrder+1)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return GetTemplateItemByID(id)
}

// GetTemplateItemByID returns a single template item by ID
func GetTemplateItemByID(id int64) (*TemplateItem, error) {
	var ti TemplateItem
	err := DB.QueryRow(`
		SELECT id, template_id, section_name, name, description, sort_order, created_at
		FROM template_items WHERE id = ?
	`, id).Scan(&ti.ID, &ti.TemplateID, &ti.SectionName, &ti.Name, &ti.Description, &ti.SortOrder, &ti.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ti, nil
}

// UpdateTemplateItem updates a template item
func UpdateTemplateItem(id int64, sectionName, name, description string) (*TemplateItem, error) {
	_, err := DB.Exec(`
		UPDATE template_items SET section_name = ?, name = ?, description = ? WHERE id = ?
	`, sectionName, name, description, id)
	if err != nil {
		return nil, err
	}
	return GetTemplateItemByID(id)
}

// DeleteTemplateItem deletes a template item
func DeleteTemplateItem(id int64) error {
	_, err := DB.Exec(`DELETE FROM template_items WHERE id = ?`, id)
	return err
}

// ApplyTemplateToList applies a template to a list (adds items from template)
func ApplyTemplateToList(templateID, listID int64) error {
	template, err := GetTemplateByID(templateID)
	if err != nil {
		return err
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Group items by section name
	sectionItems := make(map[string][]TemplateItem)
	for _, item := range template.Items {
		sectionItems[item.SectionName] = append(sectionItems[item.SectionName], item)
	}

	// For each section in template
	for sectionName, items := range sectionItems {
		// Find or create section in target list
		var sectionID int64
		err := tx.QueryRow(`
			SELECT id FROM sections WHERE list_id = ? AND name = ? COLLATE NOCASE
		`, listID, sectionName).Scan(&sectionID)

		if err != nil {
			// Section doesn't exist, create it
			var maxOrder int
			tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM sections WHERE list_id = ?", listID).Scan(&maxOrder)

			result, err := tx.Exec(`
				INSERT INTO sections (name, sort_order, list_id) VALUES (?, ?, ?)
			`, sectionName, maxOrder+1, listID)
			if err != nil {
				return err
			}
			sectionID, _ = result.LastInsertId()
		}

		// Add items to section
		for _, item := range items {
			var maxItemOrder int
			tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM items WHERE section_id = ?", sectionID).Scan(&maxItemOrder)

			_, err := tx.Exec(`
				INSERT INTO items (section_id, name, description, sort_order)
				VALUES (?, ?, ?, ?)
			`, sectionID, item.Name, item.Description, maxItemOrder+1)
			if err != nil {
				return err
			}

			// Save to item history
			tx.Exec(`
				INSERT INTO item_history (name, last_section_id, usage_count, last_used_at)
				VALUES (?, ?, 1, strftime('%s', 'now'))
				ON CONFLICT(name COLLATE NOCASE) DO UPDATE SET
					last_section_id = excluded.last_section_id,
					usage_count = usage_count + 1,
					last_used_at = strftime('%s', 'now')
			`, item.Name, sectionID)
		}
	}

	return tx.Commit()
}

// CreateTemplateFromList creates a template from an existing list
func CreateTemplateFromList(listID int64, templateName, templateDescription string) (*Template, error) {
	sections, err := GetSectionsByList(listID)
	if err != nil {
		return nil, err
	}

	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Create template
	var maxOrder int
	tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM templates").Scan(&maxOrder)

	result, err := tx.Exec(`
		INSERT INTO templates (name, description, sort_order) VALUES (?, ?, ?)
	`, templateName, templateDescription, maxOrder+1)
	if err != nil {
		return nil, err
	}
	templateID, _ := result.LastInsertId()

	// Add items from list sections
	itemOrder := 0
	for _, section := range sections {
		for _, item := range section.Items {
			if !item.Completed { // Only add non-completed items
				_, err := tx.Exec(`
					INSERT INTO template_items (template_id, section_name, name, description, sort_order)
					VALUES (?, ?, ?, ?, ?)
				`, templateID, section.Name, item.Name, item.Description, itemOrder)
				if err != nil {
					return nil, err
				}
				itemOrder++
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	return GetTemplateByID(templateID)
}

// ==================== TRANSACTION HELPERS (for batch API) ====================

// CreateListTx creates a list within a transaction
func CreateListTx(tx *sql.Tx, name, icon string) (*List, error) {
	var maxOrder int
	tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM lists").Scan(&maxOrder)

	if icon == "" {
		icon = "🛒"
	}

	result, err := tx.Exec(`
		INSERT INTO lists (name, icon, sort_order, is_active) VALUES (?, ?, ?, FALSE)
	`, name, icon, maxOrder+1)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()

	var l List
	err = tx.QueryRow(`
		SELECT id, name, COALESCE(icon, '🛒'), sort_order, is_active, created_at, COALESCE(updated_at, 0)
		FROM lists WHERE id = ?
	`, id).Scan(&l.ID, &l.Name, &l.Icon, &l.SortOrder, &l.IsActive, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// CreateSectionForListTx creates a section within a transaction
func CreateSectionForListTx(tx *sql.Tx, listID int64, name string, sortOrder int) (*Section, error) {
	result, err := tx.Exec(`
		INSERT INTO sections (name, sort_order, list_id) VALUES (?, ?, ?)
	`, name, sortOrder, listID)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()

	var s Section
	err = tx.QueryRow(`
		SELECT id, list_id, name, sort_order, COALESCE(sort_mode, 'manual'), created_at, COALESCE(updated_at, 0)
		FROM sections WHERE id = ?
	`, id).Scan(&s.ID, &s.ListID, &s.Name, &s.SortOrder, &s.SortMode, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.Items = []Item{}
	return &s, nil
}

// CreateItemTx creates an item within a transaction
func CreateItemTx(tx *sql.Tx, sectionID int64, name, description string, quantity, sortOrder int) (*Item, error) {
	result, err := tx.Exec(`
		INSERT INTO items (section_id, name, description, quantity, sort_order) VALUES (?, ?, ?, ?, ?)
	`, sectionID, name, description, quantity, sortOrder)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()

	var i Item
	err = tx.QueryRow(`
		SELECT i.id, i.section_id, i.name, i.description, i.completed, i.uncertain, COALESCE(i.quantity, 0), i.sort_order, i.created_at, COALESCE(i.updated_at, 0), COALESCE(ih.image_path, '') AS image_path
		FROM items i
		LEFT JOIN item_history ih ON i.name = ih.name COLLATE NOCASE
		WHERE i.id = ?
	`, id).Scan(&i.ID, &i.SectionID, &i.Name, &i.Description, &i.Completed, &i.Uncertain, &i.Quantity, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt, &i.ImagePath)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// SaveItemHistoryTx saves item name to history within a transaction
func SaveItemHistoryTx(tx *sql.Tx, name string, sectionID int64) {
	tx.Exec(`
		INSERT INTO item_history (name, last_section_id, usage_count)
		VALUES (?, ?, 1)
		ON CONFLICT(name) DO UPDATE SET
			usage_count = usage_count + 1,
			last_section_id = excluded.last_section_id
	`, name, sectionID)
}

// GetMaxSectionOrderTx gets max sort_order for sections in a list within a transaction
func GetMaxSectionOrderTx(tx *sql.Tx, listID int64) int {
	var maxOrder int
	tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM sections WHERE list_id = ?", listID).Scan(&maxOrder)
	return maxOrder
}

// GetMaxItemOrderTx gets max sort_order for items in a section within a transaction
func GetMaxItemOrderTx(tx *sql.Tx, sectionID int64) int {
	var maxOrder int
	tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM items WHERE section_id = ?", sectionID).Scan(&maxOrder)
	return maxOrder
}

// GetSectionIDByNameTx finds section ID by name (case-insensitive) within a transaction
// Returns 0 if section not found
func GetSectionIDByNameTx(tx *sql.Tx, sectionName string) int64 {
	if sectionName == "" {
		return 0
	}

	var sectionID int64
	err := tx.QueryRow(`
		SELECT id FROM sections
		WHERE name = ? COLLATE NOCASE
		LIMIT 1
	`, sectionName).Scan(&sectionID)

	if err != nil {
		return 0
	}
	return sectionID
}

// GetSectionNameForItem finds the section name where an item currently exists
// Used as fallback when last_section_id is not set in history
func GetSectionNameForItem(itemName string) string {
	var sectionName string
	err := DB.QueryRow(`
		SELECT s.name FROM items i
		JOIN sections s ON i.section_id = s.id
		WHERE i.name = ? COLLATE NOCASE
		LIMIT 1
	`, itemName).Scan(&sectionName)

	if err != nil {
		return ""
	}
	return sectionName
}

// ==================== RECIPES ====================

// Recipe represents a recipe with optional ingredients/steps loaded on demand.
type Recipe struct {
	ID             int64              `json:"id"`
	Name           string             `json:"name"`
	Description    string             `json:"description"`
	CoverImagePath *string            `json:"cover_image_path"`
	SortOrder      int                `json:"sort_order"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      int64              `json:"updated_at"`
	Ingredients    []RecipeIngredient `json:"ingredients,omitempty"`
	Steps          []RecipeStep       `json:"steps,omitempty"`
}

// RecipeIngredient represents one ingredient row inside a recipe.
// Quantity is *float64 because "to taste" stores NULL and recipes can use
// fractional amounts (0.5 cup, 1.25 lb, etc.).
// Notes is freeform text shown after a middle-dot in the row (e.g. "15 oz cans").
// ImagePath is populated by joining item_history.image_path on name (case-insensitive),
// so ingredient images and shopping-list-item images stay in sync via the same row.
type RecipeIngredient struct {
	ID        int64     `json:"id"`
	RecipeID  int64     `json:"recipe_id"`
	Name      string    `json:"name"`
	Quantity  *float64  `json:"quantity"`
	Unit      string    `json:"unit"`
	Notes     string    `json:"notes"`
	ImagePath string    `json:"image_path"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

// RecipeStep represents one numbered instruction step inside a recipe.
// Completed is a per-step checkbox; resets via the "Reset all steps" UI button.
type RecipeStep struct {
	ID         int64     `json:"id"`
	RecipeID   int64     `json:"recipe_id"`
	StepNumber int       `json:"step_number"`
	Content    string    `json:"content"`
	Completed  bool      `json:"completed"`
	CreatedAt  time.Time `json:"created_at"`
}

// scanRecipe is a tiny helper that knows how to read cover_image_path as nullable.
func scanRecipe(scanner interface {
	Scan(dest ...interface{}) error
}) (Recipe, error) {
	var r Recipe
	var cover sql.NullString
	if err := scanner.Scan(&r.ID, &r.Name, &r.Description, &cover, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return r, err
	}
	if cover.Valid {
		s := cover.String
		r.CoverImagePath = &s
	}
	return r, nil
}

// GetRecipes returns all recipes (no ingredients/steps loaded).
func GetRecipes() ([]Recipe, error) {
	rows, err := DB.Query(`
		SELECT id, name, description, cover_image_path, sort_order, created_at, COALESCE(updated_at, 0)
		FROM recipes
		ORDER BY sort_order ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recipes []Recipe
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			return nil, err
		}
		recipes = append(recipes, r)
	}
	return recipes, rows.Err()
}

// GetRecipe returns one recipe with its ingredients and steps.
func GetRecipe(id int64) (*Recipe, error) {
	row := DB.QueryRow(`
		SELECT id, name, description, cover_image_path, sort_order, created_at, COALESCE(updated_at, 0)
		FROM recipes WHERE id = ?
	`, id)
	r, err := scanRecipe(row)
	if err != nil {
		return nil, err
	}
	r.Ingredients, err = GetRecipeIngredients(r.ID)
	if err != nil {
		return nil, err
	}
	r.Steps, err = GetRecipeSteps(r.ID)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetRecipeIngredients returns ingredients for a recipe ordered by sort_order.
// LEFT JOIN item_history populates image_path so ingredient thumbnails come
// from the shared image-by-name pool (item_history.image_path).
func GetRecipeIngredients(recipeID int64) ([]RecipeIngredient, error) {
	rows, err := DB.Query(`
		SELECT ri.id, ri.recipe_id, ri.name, ri.quantity, ri.unit,
		       COALESCE(ri.notes, '') AS notes, ri.sort_order, ri.created_at,
		       COALESCE(ih.image_path, '') AS image_path
		FROM recipe_ingredients ri
		LEFT JOIN item_history ih ON ri.name = ih.name COLLATE NOCASE
		WHERE ri.recipe_id = ?
		ORDER BY ri.sort_order ASC
	`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ingredients []RecipeIngredient
	for rows.Next() {
		var ri RecipeIngredient
		var qty sql.NullFloat64
		if err := rows.Scan(&ri.ID, &ri.RecipeID, &ri.Name, &qty, &ri.Unit, &ri.Notes, &ri.SortOrder, &ri.CreatedAt, &ri.ImagePath); err != nil {
			return nil, err
		}
		if qty.Valid {
			q := qty.Float64
			ri.Quantity = &q
		}
		ingredients = append(ingredients, ri)
	}
	return ingredients, rows.Err()
}

// GetRecipeSteps returns steps for a recipe ordered by step_number.
func GetRecipeSteps(recipeID int64) ([]RecipeStep, error) {
	rows, err := DB.Query(`
		SELECT id, recipe_id, step_number, content, COALESCE(completed, FALSE), created_at
		FROM recipe_steps
		WHERE recipe_id = ?
		ORDER BY step_number ASC
	`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []RecipeStep
	for rows.Next() {
		var s RecipeStep
		if err := rows.Scan(&s.ID, &s.RecipeID, &s.StepNumber, &s.Content, &s.Completed, &s.CreatedAt); err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	return steps, rows.Err()
}

// CreateRecipe creates a new recipe and returns the freshly loaded row.
func CreateRecipe(name, description string) (*Recipe, error) {
	var maxOrder int
	DB.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM recipes").Scan(&maxOrder)

	result, err := DB.Exec(`
		INSERT INTO recipes (name, description, sort_order) VALUES (?, ?, ?)
	`, name, description, maxOrder+1)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return GetRecipe(id)
}

// UpdateRecipe updates a recipe's name and description.
func UpdateRecipe(id int64, name, description string) error {
	_, err := DB.Exec(`
		UPDATE recipes SET name = ?, description = ?, updated_at = strftime('%s', 'now')
		WHERE id = ?
	`, name, description, id)
	return err
}

// DeleteRecipe deletes a recipe; FK CASCADE removes its ingredients and steps.
func DeleteRecipe(id int64) error {
	_, err := DB.Exec(`DELETE FROM recipes WHERE id = ?`, id)
	return err
}

// ToggleRecipeStepCompleted flips the completed bool on a step and returns the new state.
func ToggleRecipeStepCompleted(stepID int64) (bool, error) {
	tx, err := DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var current bool
	if err := tx.QueryRow(`SELECT COALESCE(completed, FALSE) FROM recipe_steps WHERE id = ?`, stepID).Scan(&current); err != nil {
		return false, err
	}
	newState := !current
	if _, err := tx.Exec(`UPDATE recipe_steps SET completed = ? WHERE id = ?`, newState, stepID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return newState, nil
}

// ResetRecipeStepsCompleted marks all steps for a recipe as not-completed.
func ResetRecipeStepsCompleted(recipeID int64) error {
	_, err := DB.Exec(`UPDATE recipe_steps SET completed = FALSE WHERE recipe_id = ?`, recipeID)
	return err
}

// SetListCoverImage sets or clears (path == nil) the cover image path on a list.
// Mirrors SetRecipeCoverImage.
func SetListCoverImage(id int64, path *string) error {
	if path == nil {
		_, err := DB.Exec(`UPDATE lists SET cover_image_path = NULL, updated_at = strftime('%s', 'now') WHERE id = ?`, id)
		return err
	}
	_, err := DB.Exec(`UPDATE lists SET cover_image_path = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, *path, id)
	return err
}

// GetListCoverImage returns the current cover_image_path filename (empty string if NULL).
// Used by the list-cover delete handler to clean up the old file from disk.
func GetListCoverImage(id int64) (string, error) {
	var p sql.NullString
	if err := DB.QueryRow(`SELECT cover_image_path FROM lists WHERE id = ?`, id).Scan(&p); err != nil {
		return "", err
	}
	if !p.Valid {
		return "", nil
	}
	return p.String, nil
}

// SetRecipeCoverImage sets or clears (path == nil) the cover image path.
func SetRecipeCoverImage(id int64, path *string) error {
	if path == nil {
		_, err := DB.Exec(`UPDATE recipes SET cover_image_path = NULL, updated_at = strftime('%s', 'now') WHERE id = ?`, id)
		return err
	}
	_, err := DB.Exec(`UPDATE recipes SET cover_image_path = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, *path, id)
	return err
}

// MoveRecipeUp moves a recipe up in sort order. Mirrors MoveListUp.
func MoveRecipeUp(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentOrder int
	err = tx.QueryRow("SELECT sort_order FROM recipes WHERE id = ?", id).Scan(&currentOrder)
	if err != nil {
		return err
	}
	if currentOrder == 0 {
		return nil
	}
	_, err = tx.Exec(`UPDATE recipes SET sort_order = sort_order + 1 WHERE sort_order = ?`, currentOrder-1)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE recipes SET sort_order = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, currentOrder-1, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MoveRecipeDown moves a recipe down in sort order. Mirrors MoveListDown.
func MoveRecipeDown(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentOrder, maxOrder int
	err = tx.QueryRow("SELECT sort_order FROM recipes WHERE id = ?", id).Scan(&currentOrder)
	if err != nil {
		return err
	}
	err = tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM recipes").Scan(&maxOrder)
	if err != nil {
		return err
	}
	if currentOrder >= maxOrder {
		return nil
	}
	_, err = tx.Exec(`UPDATE recipes SET sort_order = sort_order - 1 WHERE sort_order = ?`, currentOrder+1)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE recipes SET sort_order = ?, updated_at = strftime('%s', 'now') WHERE id = ?`, currentOrder+1, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// AddRecipeIngredient adds an ingredient to a recipe.
// quantity is *float64 so callers pass nil for "to taste"; for all other units
// it's a positive REAL (decimals like 0.5 supported).
// notes is freeform text shown alongside the ingredient on the recipe row.
func AddRecipeIngredient(recipeID int64, name string, quantity *float64, unit, notes string) (*RecipeIngredient, error) {
	var maxOrder int
	DB.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM recipe_ingredients WHERE recipe_id = ?", recipeID).Scan(&maxOrder)

	var qtyArg interface{}
	if quantity == nil {
		qtyArg = nil
	} else {
		qtyArg = *quantity
	}

	result, err := DB.Exec(`
		INSERT INTO recipe_ingredients (recipe_id, name, quantity, unit, notes, sort_order)
		VALUES (?, ?, ?, ?, ?, ?)
	`, recipeID, name, qtyArg, unit, notes, maxOrder+1)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return GetRecipeIngredient(id)
}

// GetRecipeIngredient returns a single ingredient by ID.
// LEFT JOIN item_history populates image_path on the embedded ingredient.
func GetRecipeIngredient(id int64) (*RecipeIngredient, error) {
	var ri RecipeIngredient
	var qty sql.NullFloat64
	err := DB.QueryRow(`
		SELECT ri.id, ri.recipe_id, ri.name, ri.quantity, ri.unit,
		       COALESCE(ri.notes, '') AS notes, ri.sort_order, ri.created_at,
		       COALESCE(ih.image_path, '') AS image_path
		FROM recipe_ingredients ri
		LEFT JOIN item_history ih ON ri.name = ih.name COLLATE NOCASE
		WHERE ri.id = ?
	`, id).Scan(&ri.ID, &ri.RecipeID, &ri.Name, &qty, &ri.Unit, &ri.Notes, &ri.SortOrder, &ri.CreatedAt, &ri.ImagePath)
	if err != nil {
		return nil, err
	}
	if qty.Valid {
		q := qty.Float64
		ri.Quantity = &q
	}
	return &ri, nil
}

// UpdateRecipeIngredient updates an ingredient's name, quantity, unit, and notes.
func UpdateRecipeIngredient(id int64, name string, quantity *float64, unit, notes string) error {
	var qtyArg interface{}
	if quantity == nil {
		qtyArg = nil
	} else {
		qtyArg = *quantity
	}
	_, err := DB.Exec(`
		UPDATE recipe_ingredients SET name = ?, quantity = ?, unit = ?, notes = ?
		WHERE id = ?
	`, name, qtyArg, unit, notes, id)
	return err
}

// DeleteRecipeIngredient deletes an ingredient.
func DeleteRecipeIngredient(id int64) error {
	_, err := DB.Exec(`DELETE FROM recipe_ingredients WHERE id = ?`, id)
	return err
}

// ReorderRecipeIngredients sets sort_order based on each ID's position in orderedIDs.
// All IDs must belong to the given recipe; mismatches are rejected before any update.
func ReorderRecipeIngredients(recipeID int64, orderedIDs []int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, id := range orderedIDs {
		var owner int64
		if err := tx.QueryRow("SELECT recipe_id FROM recipe_ingredients WHERE id = ?", id).Scan(&owner); err != nil {
			return fmt.Errorf("ingredient %d not found: %w", id, err)
		}
		if owner != recipeID {
			return fmt.Errorf("ingredient %d does not belong to recipe %d", id, recipeID)
		}
	}

	for pos, id := range orderedIDs {
		if _, err := tx.Exec(`UPDATE recipe_ingredients SET sort_order = ? WHERE id = ?`, pos, id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// AddRecipeStep appends a step to a recipe with step_number = MAX+1 (1-based).
func AddRecipeStep(recipeID int64, content string) (*RecipeStep, error) {
	var maxStep int
	DB.QueryRow("SELECT COALESCE(MAX(step_number), 0) FROM recipe_steps WHERE recipe_id = ?", recipeID).Scan(&maxStep)

	result, err := DB.Exec(`
		INSERT INTO recipe_steps (recipe_id, step_number, content) VALUES (?, ?, ?)
	`, recipeID, maxStep+1, content)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return GetRecipeStep(id)
}

// GetRecipeStep returns a single step by ID.
func GetRecipeStep(id int64) (*RecipeStep, error) {
	var s RecipeStep
	err := DB.QueryRow(`
		SELECT id, recipe_id, step_number, content, COALESCE(completed, FALSE), created_at
		FROM recipe_steps WHERE id = ?
	`, id).Scan(&s.ID, &s.RecipeID, &s.StepNumber, &s.Content, &s.Completed, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateRecipeStep updates a step's content (step_number is managed by reorder/delete).
func UpdateRecipeStep(id int64, content string) error {
	_, err := DB.Exec(`UPDATE recipe_steps SET content = ? WHERE id = ?`, content, id)
	return err
}

// DeleteRecipeStep deletes a step and renumbers the remaining steps so there are no gaps.
func DeleteRecipeStep(id int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var recipeID int64
	if err := tx.QueryRow("SELECT recipe_id FROM recipe_steps WHERE id = ?", id).Scan(&recipeID); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM recipe_steps WHERE id = ?", id); err != nil {
		return err
	}

	rows, err := tx.Query(`
		SELECT id FROM recipe_steps
		WHERE recipe_id = ?
		ORDER BY step_number ASC
	`, recipeID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var sid int64
		if err := rows.Scan(&sid); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, sid)
	}
	rows.Close()

	for i, sid := range ids {
		if _, err := tx.Exec(`UPDATE recipe_steps SET step_number = ? WHERE id = ?`, i+1, sid); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ReorderRecipeSteps assigns step_number = position+1 for each ID in orderedIDs.
// All IDs must belong to the given recipe.
func ReorderRecipeSteps(recipeID int64, orderedIDs []int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, id := range orderedIDs {
		var owner int64
		if err := tx.QueryRow("SELECT recipe_id FROM recipe_steps WHERE id = ?", id).Scan(&owner); err != nil {
			return fmt.Errorf("step %d not found: %w", id, err)
		}
		if owner != recipeID {
			return fmt.Errorf("step %d does not belong to recipe %d", id, recipeID)
		}
	}

	// Two-phase update to avoid step_number collisions: shift everyone into a
	// disjoint range first, then assign final values.
	if _, err := tx.Exec(`UPDATE recipe_steps SET step_number = step_number + 100000 WHERE recipe_id = ?`, recipeID); err != nil {
		return err
	}
	for pos, id := range orderedIDs {
		if _, err := tx.Exec(`UPDATE recipe_steps SET step_number = ? WHERE id = ?`, pos+1, id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// findItemByNameInListTx finds an item in any section of the given list (case-insensitive name match).
// Returns nil, nil if no match. Used by ApplyRecipeToList.
func findItemByNameInListTx(tx *sql.Tx, listID int64, name string) (*Item, error) {
	var i Item
	err := tx.QueryRow(`
		SELECT i.id, i.section_id, i.name, i.description, i.completed, i.uncertain,
		       COALESCE(i.quantity, 0), i.sort_order, i.created_at, COALESCE(i.updated_at, 0)
		FROM items i
		JOIN sections s ON s.id = i.section_id
		WHERE s.list_id = ? AND LOWER(i.name) = LOWER(?)
		LIMIT 1
	`, listID, name).Scan(&i.ID, &i.SectionID, &i.Name, &i.Description, &i.Completed, &i.Uncertain,
		&i.Quantity, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

// findOrCreateRecipeIngredientsSectionTx finds (or creates) the "Recipe ingredients"
// fallback section in the given list. Used when no history hint is available.
func findOrCreateRecipeIngredientsSectionTx(tx *sql.Tx, listID int64) (int64, error) {
	const fallbackName = "Recipe ingredients"

	var existing int64
	err := tx.QueryRow(`
		SELECT id FROM sections WHERE list_id = ? AND LOWER(name) = LOWER(?) LIMIT 1
	`, listID, fallbackName).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	var maxOrder int
	tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM sections WHERE list_id = ?", listID).Scan(&maxOrder)

	result, err := tx.Exec(`
		INSERT INTO sections (name, sort_order, list_id) VALUES (?, ?, ?)
	`, fallbackName, maxOrder+1, listID)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return id, nil
}

// pickSectionForIngredientTx decides which section a NEW item should land in.
// 1) If item_history.last_section_id points to a section in this list, use it.
// 2) Otherwise fall back to the auto-created "Recipe ingredients" section.
func pickSectionForIngredientTx(tx *sql.Tx, listID int64, name string) (int64, error) {
	var lastSection sql.NullInt64
	tx.QueryRow(`
		SELECT last_section_id FROM item_history WHERE name = ? COLLATE NOCASE LIMIT 1
	`, name).Scan(&lastSection)

	if lastSection.Valid && lastSection.Int64 > 0 {
		var ownerList int64
		err := tx.QueryRow(`SELECT list_id FROM sections WHERE id = ?`, lastSection.Int64).Scan(&ownerList)
		if err == nil && ownerList == listID {
			return lastSection.Int64, nil
		}
	}

	return findOrCreateRecipeIngredientsSectionTx(tx, listID)
}

// MeasurementUnits are cooking/measurement units. When applying a recipe to a
// list, list-qty stays at 1 and the recipe amount goes into description.
// Multiple measurement-unit ingredients with the same unit auto-combine (e.g.
// "1 cup" + "1 cup" -> "2 cup").
var MeasurementUnits = map[string]bool{
	"tsp":   true,
	"tbsp":  true,
	"cup":   true,
	"fl_oz": true,
	"oz":    true,
	"lb":    true,
	"g":     true,
	"kg":    true,
	"ml":    true,
	"l":     true,
}

// PackageUnits are discrete/package units. List-qty multiplies (qty = ceil of
// recipe qty). Description gets unit + notes; "whole" hides the unit entirely.
var PackageUnits = map[string]bool{
	"whole":   true,
	"can":     true,
	"jar":     true,
	"bottle":  true,
	"package": true,
	"bunch":   true,
	"head":    true,
	"dozen":   true,
	"slice":   true,
	"loaf":    true,
	"clove":   true,
}

// formatUnitDescription mirrors handlers.formatUnitForDescription.
// Defined here too because db/queries.go can't import the handlers package.
func formatUnitDescription(unit string) string {
	switch unit {
	case "to_taste":
		return "to taste"
	case "fl_oz":
		return "fl oz"
	default:
		return unit
	}
}

// formatQtyForDescription renders a float quantity into a compact string for
// item descriptions. Mirrors the JS formatQuantity helper in static/recipe.js
// so server-rendered descriptions match what users see on the recipe page.
//   2.0   -> "2"
//   0.5   -> "½"
//   1.5   -> "1½"
//   2.75  -> "2¾"
//   1/3   -> "⅓" (0.33 or 0.34)
//   1.7   -> "1.7"   (no glyph, falls back to up-to-2-decimal)
//   0.123 -> "0.12"
func formatQtyForDescription(q float64) string {
	if q == math.Trunc(q) {
		return strconv.FormatFloat(q, 'f', 0, 64)
	}
	whole := math.Trunc(q)
	frac := math.Round((q-whole)*100) / 100
	glyph := fractionGlyph(frac)
	if glyph != "" {
		if whole == 0 {
			return glyph
		}
		return strconv.FormatFloat(whole, 'f', 0, 64) + glyph
	}
	s := strconv.FormatFloat(q, 'f', 2, 64)
	// Trim trailing zeros after decimal point.
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// fractionGlyph maps the common quarter/third fractions to their unicode
// glyph; returns "" for anything else so the caller can fall back to decimals.
// Both 0.33 and 0.34 map to ⅓ because rounding to 2 decimals can give either.
func fractionGlyph(frac float64) string {
	switch frac {
	case 0.25:
		return "¼"
	case 0.5:
		return "½"
	case 0.75:
		return "¾"
	case 0.33, 0.34:
		return "⅓"
	case 0.66, 0.67:
		return "⅔"
	}
	return ""
}

// joinNotesIntoDescription appends notes after a middle-dot separator if both
// sides are non-empty. Mirrors the recipe-row UI ("½ cup Sugar · 15 oz cans").
// The result is run through dedupDescriptionSegments so re-applying a recipe
// with identical notes doesn't accumulate "for sweetness · for sweetness".
func joinNotesIntoDescription(left, notes string) string {
	if notes == "" {
		return dedupDescriptionSegments(left)
	}
	if left == "" {
		return dedupDescriptionSegments(notes)
	}
	return dedupDescriptionSegments(left + " · " + notes)
}

// dedupDescriptionSegments splits an item description on " · " and removes
// duplicate segments while preserving order. Comparison is case-insensitive
// on trimmed segments; the original casing of the first occurrence is kept.
//
// Used by every description-building path in ApplyRecipeToList so that
// re-applying a recipe with the same notes ("for sweetness") doesn't double up
// the segment list. Empty input returns empty.
func dedupDescriptionSegments(desc string) string {
	if desc == "" {
		return ""
	}
	parts := strings.Split(desc, " · ")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		key := strings.ToLower(strings.TrimSpace(p))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return strings.Join(out, " · ")
}

// existingMeasurementParse tries to read a "<qty> <unit>" prefix from an
// existing item.description so apply-to-list can combine same-unit amounts.
// Returns (qty, unitToken, restOfDescription, true) on success.
//
// Accepts: integer ("2"), decimal ("1.5"), bare fraction ("1/2"), unicode
// fractions ("¼ ½ ¾ ⅓ ⅔"), and mixed forms ("1¼", "1 1/2"). On parse failure
// returns (_,_,_,false) and the caller falls back to concatenation.
func existingMeasurementParse(desc string) (float64, string, string, bool) {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return 0, "", "", false
	}

	// Split off the leading qty token (everything up to the first space).
	// Then take the next whitespace-separated token as the unit.
	parts := strings.SplitN(desc, " ", 3)
	if len(parts) < 2 {
		return 0, "", "", false
	}
	qtyTok := parts[0]
	unitTok := parts[1]
	rest := ""
	if len(parts) == 3 {
		rest = parts[2]
	}

	q, ok := parseQtyToken(qtyTok)
	if !ok {
		// Maybe the qty is "1 1/2" — split-by-3 already captured 2 tokens.
		// Try interpreting parts[0]+" "+parts[1] as a mixed number, parts[2] as unit.
		if len(parts) == 3 {
			if mixed, ok2 := parseQtyToken(qtyTok + " " + unitTok); ok2 {
				// Re-split rest into "unit" + remaining.
				rp := strings.SplitN(rest, " ", 2)
				if len(rp) == 0 {
					return 0, "", "", false
				}
				newUnit := rp[0]
				newRest := ""
				if len(rp) == 2 {
					newRest = rp[1]
				}
				return mixed, newUnit, newRest, true
			}
		}
		return 0, "", "", false
	}
	return q, unitTok, rest, true
}

// parseQtyToken handles integer, decimal, bare fraction (1/2), single unicode
// fraction glyphs, and mixed forms ("1¼", "1 1/2").
func parseQtyToken(tok string) (float64, bool) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return 0, false
	}

	// Plain integer or decimal.
	if v, err := strconv.ParseFloat(tok, 64); err == nil {
		return v, true
	}

	// Bare fraction "1/2".
	if v, ok := parseBareFraction(tok); ok {
		return v, true
	}

	// Single unicode fraction.
	if v, ok := unicodeFractionValue(tok); ok {
		return v, true
	}

	// Mixed: leading integer + trailing unicode fraction ("1¼").
	for i, r := range tok {
		// Look for the first non-digit character.
		if r < '0' || r > '9' {
			intPart := tok[:i]
			fracPart := tok[i:]
			if intPart == "" {
				return 0, false
			}
			intVal, err := strconv.Atoi(intPart)
			if err != nil {
				return 0, false
			}
			if v, ok := unicodeFractionValue(fracPart); ok {
				return float64(intVal) + v, true
			}
			if v, ok := parseBareFraction(fracPart); ok {
				return float64(intVal) + v, true
			}
			return 0, false
		}
	}

	// Mixed: "1 1/2" (came in as a single concatenated token "1 1/2" — handled
	// by recipientParse already; here we split on space ourselves).
	if sp := strings.SplitN(tok, " ", 2); len(sp) == 2 {
		i, ierr := strconv.Atoi(sp[0])
		f, fok := parseBareFraction(sp[1])
		if ierr == nil && fok {
			return float64(i) + f, true
		}
	}

	return 0, false
}

func parseBareFraction(s string) (float64, bool) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0, false
	}
	num, err1 := strconv.Atoi(parts[0])
	den, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || den == 0 {
		return 0, false
	}
	return float64(num) / float64(den), true
}

func unicodeFractionValue(s string) (float64, bool) {
	switch s {
	case "¼":
		return 0.25, true
	case "½":
		return 0.5, true
	case "¾":
		return 0.75, true
	case "⅓":
		return 1.0 / 3.0, true
	case "⅔":
		return 2.0 / 3.0, true
	}
	return 0, false
}

// ApplyRecipeToList adds the chosen ingredients from a recipe to the target list.
//
// Per-ingredient behavior depends on the unit type:
//
// MEASUREMENT units (tsp, tbsp, cup, fl_oz, oz, lb, g, kg, ml, l):
//   - New item: qty = 1, description = "<recipe_qty> <unit>" + " · <notes>".
//   - Existing item: qty unchanged. If the existing description starts with
//     "<num> <same_unit>", combine numerically: "(old+new) <unit>" + merged notes.
//     Otherwise append " + <recipe_qty> <unit>" to the existing description.
//
// PACKAGE units (whole, can, jar, bottle, package, bunch, head, dozen, slice,
// loaf, clove):
//   - New item: qty = ceil(recipe_qty), description = "<unit>" + " · <notes>".
//     Special case for "whole": unit hidden, only notes shown.
//   - Existing item: qty += ceil(recipe_qty). Description left alone.
//
// to_taste:
//   - New item: qty = 1, description = "to taste" + " · <notes>".
//   - Existing item: no change.
//
// Reactivation: if the matching item is completed, it gets reactivated
// (completed=false) regardless of unit type, mirroring CreateItem behavior.
//
// All work happens in a single transaction.
func ApplyRecipeToList(recipeID, targetListID int64, ingredientIDs []int64) error {
	if len(ingredientIDs) == 0 {
		return fmt.Errorf("no ingredients selected")
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var listExists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM lists WHERE id = ?`, targetListID).Scan(&listExists); err != nil {
		return err
	}
	if listExists == 0 {
		return fmt.Errorf("target list %d not found", targetListID)
	}

	placeholders := make([]string, len(ingredientIDs))
	args := []interface{}{recipeID}
	for i, id := range ingredientIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT id, recipe_id, name, quantity, unit, COALESCE(notes, ''), sort_order
		FROM recipe_ingredients
		WHERE recipe_id = ? AND id IN (%s)
		ORDER BY sort_order ASC
	`, strings.Join(placeholders, ","))

	rows, err := tx.Query(query, args...)
	if err != nil {
		return err
	}
	type chosen struct {
		Name     string
		Quantity *float64
		Unit     string
		Notes    string
	}
	var picks []chosen
	for rows.Next() {
		var id, rid int64
		var name, unit, notes string
		var qty sql.NullFloat64
		var sortOrder int
		if err := rows.Scan(&id, &rid, &name, &qty, &unit, &notes, &sortOrder); err != nil {
			rows.Close()
			return err
		}
		c := chosen{Name: name, Unit: unit, Notes: notes}
		if qty.Valid {
			q := qty.Float64
			c.Quantity = &q
		}
		picks = append(picks, c)
	}
	rows.Close()

	if len(picks) == 0 {
		return fmt.Errorf("no matching ingredients found in recipe")
	}

	for _, pick := range picks {
		existing, err := findItemByNameInListTx(tx, targetListID, pick.Name)
		if err != nil {
			return err
		}

		switch {
		case pick.Unit == "to_taste":
			if existing != nil {
				if existing.Completed {
					if _, err := tx.Exec(`
						UPDATE items SET completed = FALSE, updated_at = strftime('%s','now')
						WHERE id = ?
					`, existing.ID); err != nil {
						return err
					}
				}
				SaveItemHistoryTx(tx, pick.Name, existing.SectionID)
				continue
			}
			desc := joinNotesIntoDescription("to taste", pick.Notes)
			if err := insertNewRecipeItem(tx, targetListID, pick.Name, desc, 1); err != nil {
				return err
			}

		case PackageUnits[pick.Unit]:
			recQty := 0.0
			if pick.Quantity != nil {
				recQty = *pick.Quantity
			}
			addQty := int(math.Ceil(recQty))
			if addQty < 1 {
				addQty = 1
			}
			if existing != nil {
				newQty := existing.Quantity + addQty
				if existing.Completed {
					if _, err := tx.Exec(`
						UPDATE items SET completed = FALSE, quantity = ?, updated_at = strftime('%s','now')
						WHERE id = ?
					`, newQty, existing.ID); err != nil {
						return err
					}
				} else {
					if _, err := tx.Exec(`
						UPDATE items SET quantity = ?, updated_at = strftime('%s','now')
						WHERE id = ?
					`, newQty, existing.ID); err != nil {
						return err
					}
				}
				SaveItemHistoryTx(tx, pick.Name, existing.SectionID)
				continue
			}
			// New item — for "whole" we hide the unit AND the qty (just notes,
			// e.g. "large, white" for "1 large white onion"). For all other
			// packaging units we preserve the original recipe quantity so the
			// user can still see "1½ dozen" or "2 can" even though the list-side
			// quantity is the ceiled integer count.
			descLeft := ""
			if pick.Unit != "whole" {
				if recQty > 0 {
					descLeft = formatQtyForDescription(recQty) + " " + formatUnitDescription(pick.Unit)
				} else {
					descLeft = formatUnitDescription(pick.Unit)
				}
			}
			desc := joinNotesIntoDescription(descLeft, pick.Notes)
			if err := insertNewRecipeItem(tx, targetListID, pick.Name, desc, addQty); err != nil {
				return err
			}

		case MeasurementUnits[pick.Unit]:
			recQty := 0.0
			if pick.Quantity != nil {
				recQty = *pick.Quantity
			}
			if existing != nil {
				newDesc := combineMeasurementDescription(existing.Description, recQty, pick.Unit, pick.Notes)
				if existing.Completed {
					if _, err := tx.Exec(`
						UPDATE items SET completed = FALSE, description = ?, updated_at = strftime('%s','now')
						WHERE id = ?
					`, newDesc, existing.ID); err != nil {
						return err
					}
				} else {
					if _, err := tx.Exec(`
						UPDATE items SET description = ?, updated_at = strftime('%s','now')
						WHERE id = ?
					`, newDesc, existing.ID); err != nil {
						return err
					}
				}
				SaveItemHistoryTx(tx, pick.Name, existing.SectionID)
				continue
			}
			descLeft := formatQtyForDescription(recQty) + " " + formatUnitDescription(pick.Unit)
			desc := joinNotesIntoDescription(descLeft, pick.Notes)
			if err := insertNewRecipeItem(tx, targetListID, pick.Name, desc, 1); err != nil {
				return err
			}

		default:
			// Unknown unit — defensive fallback (should not happen because handler
			// validates unit before reaching here).
			descLeft := formatUnitDescription(pick.Unit)
			desc := joinNotesIntoDescription(descLeft, pick.Notes)
			if existing == nil {
				if err := insertNewRecipeItem(tx, targetListID, pick.Name, desc, 1); err != nil {
					return err
				}
			}
		}
	}

	return tx.Commit()
}

// insertNewRecipeItem is the new-item shared path used by all three unit-type
// branches in ApplyRecipeToList. Picks a section, computes sort order, inserts,
// and bumps history.
func insertNewRecipeItem(tx *sql.Tx, listID int64, name, description string, quantity int) error {
	sectionID, err := pickSectionForIngredientTx(tx, listID, name)
	if err != nil {
		return err
	}
	var maxItemOrder int
	tx.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM items WHERE section_id = ?", sectionID).Scan(&maxItemOrder)

	if _, err := tx.Exec(`
		INSERT INTO items (section_id, name, description, quantity, sort_order)
		VALUES (?, ?, ?, ?, ?)
	`, sectionID, name, description, quantity, maxItemOrder+1); err != nil {
		return err
	}
	SaveItemHistoryTx(tx, name, sectionID)
	return nil
}

// combineMeasurementDescription is the apply-to-list combiner for measurement
// units. If the existing description starts with "<num> <same_unit>", combines
// numerically and merges notes. Otherwise appends "+ <new_qty> <unit>" to keep
// both pieces of info.
//
// existing might also include " · notes" suffix from a previous apply; the
// parser handles that by treating everything after the unit token as `rest`.
func combineMeasurementDescription(existingDesc string, addQty float64, unit, newNotes string) string {
	addLeft := formatQtyForDescription(addQty) + " " + formatUnitDescription(unit)
	addBlock := joinNotesIntoDescription(addLeft, newNotes)

	q, parsedUnit, rest, ok := existingMeasurementParse(existingDesc)
	if !ok {
		// Existing isn't a "<qty> <unit>" prefix at all — append.
		// The dedup pass on the right-hand side already collapsed identical
		// notes inside addBlock; nothing to do here for the "+" join itself
		// since segments on either side of " + " are intentionally separate.
		if existingDesc == "" {
			return addBlock
		}
		return dedupDescriptionSegments(existingDesc) + " + " + addBlock
	}

	if parsedUnit != formatUnitDescription(unit) {
		// Different unit — append.
		return dedupDescriptionSegments(existingDesc) + " + " + addBlock
	}

	// Same unit — combine. Sum the quantities; preserve trailing notes from
	// either side. `rest` from the parse may include leading " · old notes".
	sum := q + addQty
	combinedLeft := formatQtyForDescription(sum) + " " + formatUnitDescription(unit)

	// Pull notes off the existing description's `rest` block (everything after
	// the leading "· "). This keeps the shape "<qty> <unit> · notes" idempotent.
	existingNotes := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "·"))

	mergedNotes := existingNotes
	if newNotes != "" {
		if mergedNotes == "" {
			mergedNotes = newNotes
		} else {
			mergedNotes = mergedNotes + " · " + newNotes
		}
	}
	return joinNotesIntoDescription(combinedLeft, mergedNotes)
}

// ==================== DATABASE CLEAR ====================

// ClearAllData clears all user data from database (lists, sections, items, templates, history)
// Sessions are preserved so user remains logged in
func ClearAllData() error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete in proper order due to foreign key constraints
	// 1. template_items (references templates)
	if _, err := tx.Exec("DELETE FROM template_items"); err != nil {
		return fmt.Errorf("failed to delete template_items: %w", err)
	}

	// 2. templates
	if _, err := tx.Exec("DELETE FROM templates"); err != nil {
		return fmt.Errorf("failed to delete templates: %w", err)
	}

	// 3. items (references sections)
	if _, err := tx.Exec("DELETE FROM items"); err != nil {
		return fmt.Errorf("failed to delete items: %w", err)
	}

	// 4. sections (references lists)
	if _, err := tx.Exec("DELETE FROM sections"); err != nil {
		return fmt.Errorf("failed to delete sections: %w", err)
	}

	// 5. lists
	if _, err := tx.Exec("DELETE FROM lists"); err != nil {
		return fmt.Errorf("failed to delete lists: %w", err)
	}

	// 6. item_history
	if _, err := tx.Exec("DELETE FROM item_history"); err != nil {
		return fmt.Errorf("failed to delete item_history: %w", err)
	}

	// 7. recipe_steps + recipe_ingredients (FK to recipes; deleted explicitly
	//    so we don't depend on PRAGMA foreign_keys cascade order)
	if _, err := tx.Exec("DELETE FROM recipe_steps"); err != nil {
		return fmt.Errorf("failed to delete recipe_steps: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM recipe_ingredients"); err != nil {
		return fmt.Errorf("failed to delete recipe_ingredients: %w", err)
	}

	// 8. recipes
	if _, err := tx.Exec("DELETE FROM recipes"); err != nil {
		return fmt.Errorf("failed to delete recipes: %w", err)
	}

	// Note: sessions are NOT deleted - user remains logged in

	return tx.Commit()
}
