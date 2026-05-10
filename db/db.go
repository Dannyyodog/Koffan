package db

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Init() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./shopping.db"
	}

	// Create parent directory if it doesn't exist
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			log.Fatal("Failed to create database directory:", err)
		}
	}

	var err error
	// Enable WAL mode and foreign keys for better concurrency
	DB, err = sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Test connection
	if err = DB.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	// SQLite serializes writes; limit pool to 1 to avoid SQLITE_BUSY errors
	// and eliminate the need for retry logic on concurrent writes.
	DB.SetMaxOpenConns(1)
	DB.SetMaxIdleConns(1)
	DB.SetConnMaxLifetime(0)

	// Enable WAL mode explicitly (in case pragma wasn't applied via connection string)
	_, err = DB.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		log.Println("Warning: Could not enable WAL mode:", err)
	}

	// Set busy timeout to 5 seconds
	_, err = DB.Exec("PRAGMA busy_timeout=5000")
	if err != nil {
		log.Println("Warning: Could not set busy timeout:", err)
	}

	// Create tables
	createTables()

	log.Println("Database initialized successfully (WAL mode)")
}

func createTables() {
	schema := `
	CREATE TABLE IF NOT EXISTS sections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		sort_order INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at INTEGER DEFAULT (strftime('%s', 'now'))
	);

	CREATE TABLE IF NOT EXISTS items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		section_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		completed BOOLEAN DEFAULT FALSE,
		uncertain BOOLEAN DEFAULT FALSE,
		sort_order INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at INTEGER DEFAULT (strftime('%s', 'now')),
		FOREIGN KEY (section_id) REFERENCES sections(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		expires_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS item_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL COLLATE NOCASE,
		last_section_id INTEGER,
		usage_count INTEGER DEFAULT 1,
		last_used_at INTEGER DEFAULT (strftime('%s', 'now')),
		UNIQUE(name COLLATE NOCASE)
	);

	CREATE INDEX IF NOT EXISTS idx_items_section ON items(section_id, sort_order);
	CREATE INDEX IF NOT EXISTS idx_sections_order ON sections(sort_order);
	CREATE INDEX IF NOT EXISTS idx_item_history_name ON item_history(name COLLATE NOCASE);
	`

	_, err := DB.Exec(schema)
	if err != nil {
		log.Fatal("Failed to create tables:", err)
	}

	// Migration: Add updated_at column if it doesn't exist
	runMigrations()
}

func runMigrations() {
	// Check if updated_at column exists in sections
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('sections') WHERE name='updated_at'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count == 0 {
		log.Println("Running migration: Adding updated_at to sections...")
		// SQLite doesn't support dynamic DEFAULT in ALTER TABLE, so add with NULL first
		_, err := DB.Exec("ALTER TABLE sections ADD COLUMN updated_at INTEGER")
		if err != nil {
			log.Println("Migration failed for sections:", err)
		} else {
			// Set updated_at for existing rows
			_, updateErr := DB.Exec("UPDATE sections SET updated_at = strftime('%s', 'now')")
			if updateErr != nil {
				log.Printf("WARNING: Migration UPDATE failed for sections: %v", updateErr)
			}
			log.Println("Migration completed: sections.updated_at added")
		}
	}

	// Check if updated_at column exists in items
	err = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('items') WHERE name='updated_at'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count == 0 {
		log.Println("Running migration: Adding updated_at to items...")
		// SQLite doesn't support dynamic DEFAULT in ALTER TABLE, so add with NULL first
		_, err := DB.Exec("ALTER TABLE items ADD COLUMN updated_at INTEGER")
		if err != nil {
			log.Println("Migration failed for items:", err)
		} else {
			// Set updated_at for existing rows
			_, updateErr := DB.Exec("UPDATE items SET updated_at = strftime('%s', 'now')")
			if updateErr != nil {
				log.Printf("WARNING: Migration UPDATE failed for items: %v", updateErr)
			}
			log.Println("Migration completed: items.updated_at added")
		}
	}

	// Migration: Multiple lists support
	migrateToMultipleLists()

	// Migration: Templates support
	migrateTemplates()

	// Migration: Add icon to lists
	migrateListIcons()

	// Migration: Add quantity to items
	migrateItemQuantity()

	// Migration: Add sort_mode to sections
	migrateSectionSortMode()

	// Migration: Add show_completed to lists
	migrateListShowCompleted()

	// Migration: Add image_path to item_history
	migrateItemHistoryImage()

	// Migration: Recipes support (recipes -> ingredients/steps via FK CASCADE)
	migrateRecipes()
	migrateRecipeIngredients()
	migrateRecipeSteps()

	// Migration: recipe_ingredients.quantity REAL + notes TEXT
	migrateRecipeIngredientsDecimal()

	// Migration: recipe_steps.completed BOOLEAN
	migrateRecipeStepsCompleted()

	// Migration: lists.cover_image_path TEXT
	migrateListsCoverImage()
}

func migrateToMultipleLists() {
	// Check if lists table exists
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='lists'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding multiple lists support...")

	// Create lists table
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS lists (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			sort_order INTEGER NOT NULL,
			is_active BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at INTEGER DEFAULT (strftime('%s', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_lists_order ON lists(sort_order);
		CREATE INDEX IF NOT EXISTS idx_lists_active ON lists(is_active);
	`)
	if err != nil {
		log.Println("Migration failed - creating lists table:", err)
		return
	}

	// Add list_id column to sections
	_, err = DB.Exec("ALTER TABLE sections ADD COLUMN list_id INTEGER REFERENCES lists(id) ON DELETE CASCADE")
	if err != nil {
		log.Println("Migration failed - adding list_id to sections:", err)
		return
	}

	// Create index for list_id
	_, err = DB.Exec("CREATE INDEX IF NOT EXISTS idx_sections_list ON sections(list_id, sort_order)")
	if err != nil {
		log.Println("Migration warning - creating sections list index:", err)
	}

	log.Println("Migration completed: Multiple lists support added")
}

func migrateTemplates() {
	// Check if templates table exists
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='templates'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding templates support...")

	// Create templates table
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS templates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			sort_order INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at INTEGER DEFAULT (strftime('%s', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_templates_order ON templates(sort_order);
	`)
	if err != nil {
		log.Println("Migration failed - creating templates table:", err)
		return
	}

	// Create template_items table
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS template_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			template_id INTEGER NOT NULL,
			section_name TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			sort_order INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_template_items_template ON template_items(template_id, sort_order);
	`)
	if err != nil {
		log.Println("Migration failed - creating template_items table:", err)
		return
	}

	log.Println("Migration completed: Templates support added")
}

func migrateListIcons() {
	// Check if icon column exists in lists
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('lists') WHERE name='icon'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding icon to lists...")

	_, err = DB.Exec("ALTER TABLE lists ADD COLUMN icon TEXT DEFAULT '🛒'")
	if err != nil {
		log.Println("Migration failed - adding icon to lists:", err)
		return
	}

	log.Println("Migration completed: List icons added")
}

func migrateItemQuantity() {
	// Check if quantity column exists in items
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('items') WHERE name='quantity'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding quantity to items...")

	_, err = DB.Exec("ALTER TABLE items ADD COLUMN quantity INTEGER DEFAULT 0")
	if err != nil {
		log.Println("Migration failed - adding quantity to items:", err)
		return
	}

	log.Println("Migration completed: Item quantity added")
}

func migrateSectionSortMode() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('sections') WHERE name='sort_mode'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding sort_mode to sections...")

	_, err = DB.Exec("ALTER TABLE sections ADD COLUMN sort_mode TEXT DEFAULT 'manual'")
	if err != nil {
		log.Println("Migration failed - adding sort_mode to sections:", err)
		return
	}

	log.Println("Migration completed: Section sort_mode added")
}

func migrateListShowCompleted() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('lists') WHERE name='show_completed'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding show_completed to lists...")

	_, err = DB.Exec("ALTER TABLE lists ADD COLUMN show_completed BOOLEAN DEFAULT TRUE")
	if err != nil {
		log.Println("Migration failed - adding show_completed to lists:", err)
		return
	}

	log.Println("Migration completed: List show_completed added")
}

func migrateItemHistoryImage() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('item_history') WHERE name='image_path'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding image_path to item_history...")

	_, err = DB.Exec("ALTER TABLE item_history ADD COLUMN image_path TEXT DEFAULT NULL")
	if err != nil {
		log.Println("Migration failed - adding image_path to item_history:", err)
		return
	}

	log.Println("Migration completed: item_history.image_path added")
}

func migrateRecipes() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='recipes'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding recipes table...")

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS recipes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			cover_image_path TEXT DEFAULT NULL,
			sort_order INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at INTEGER DEFAULT (strftime('%s', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_recipes_order ON recipes(sort_order);
	`)
	if err != nil {
		log.Println("Migration failed - creating recipes table:", err)
		return
	}

	log.Println("Migration completed: Recipes table added")
}

func migrateRecipeIngredients() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='recipe_ingredients'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding recipe_ingredients table...")

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS recipe_ingredients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			recipe_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			quantity INTEGER DEFAULT NULL,
			unit TEXT NOT NULL DEFAULT 'whole',
			sort_order INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_recipe_ingredients_recipe ON recipe_ingredients(recipe_id, sort_order);
	`)
	if err != nil {
		log.Println("Migration failed - creating recipe_ingredients table:", err)
		return
	}

	log.Println("Migration completed: Recipe ingredients table added")
}

func migrateRecipeSteps() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='recipe_steps'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding recipe_steps table...")

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS recipe_steps (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			recipe_id INTEGER NOT NULL,
			step_number INTEGER NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_recipe_steps_recipe ON recipe_steps(recipe_id, step_number);
	`)
	if err != nil {
		log.Println("Migration failed - creating recipe_steps table:", err)
		return
	}

	log.Println("Migration completed: Recipe steps table added")
}

// migrateRecipeIngredientsDecimal handles two changes at once:
//  1. quantity INTEGER -> REAL (so recipes can store decimals like 0.5 cup)
//  2. notes TEXT DEFAULT '' (new column)
//
// SQLite doesn't support changing a column type via ALTER TABLE, so we use the
// standard rename-and-recreate dance. Probe is "quantity column type is still
// INTEGER OR notes column doesn't exist" — once both are fixed, this no-ops.
func migrateRecipeIngredientsDecimal() {
	var qtyType string
	if err := DB.QueryRow("SELECT type FROM pragma_table_info('recipe_ingredients') WHERE name='quantity'").Scan(&qtyType); err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	var notesCount int
	if err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('recipe_ingredients') WHERE name='notes'").Scan(&notesCount); err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	// Already migrated when quantity is REAL AND notes already exists.
	if qtyType == "REAL" && notesCount > 0 {
		return
	}

	log.Println("Running migration: Converting recipe_ingredients.quantity to REAL and adding notes...")

	tx, err := DB.Begin()
	if err != nil {
		log.Println("Migration failed - begin:", err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE recipe_ingredients_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			recipe_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			quantity REAL DEFAULT NULL,
			unit TEXT NOT NULL DEFAULT 'whole',
			notes TEXT DEFAULT '',
			sort_order INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE
		);
	`); err != nil {
		log.Println("Migration failed - creating recipe_ingredients_new:", err)
		return
	}

	// Copy old rows. notes defaults to '' for pre-migration rows. CAST keeps
	// existing integer quantities as REAL (e.g. 4 -> 4.0).
	if _, err := tx.Exec(`
		INSERT INTO recipe_ingredients_new (id, recipe_id, name, quantity, unit, notes, sort_order, created_at)
		SELECT id, recipe_id, name, CAST(quantity AS REAL), unit, '' AS notes, sort_order, created_at
		FROM recipe_ingredients
	`); err != nil {
		log.Println("Migration failed - copying recipe_ingredients:", err)
		return
	}

	if _, err := tx.Exec(`DROP TABLE recipe_ingredients`); err != nil {
		log.Println("Migration failed - dropping old recipe_ingredients:", err)
		return
	}
	if _, err := tx.Exec(`ALTER TABLE recipe_ingredients_new RENAME TO recipe_ingredients`); err != nil {
		log.Println("Migration failed - renaming recipe_ingredients_new:", err)
		return
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_recipe_ingredients_recipe ON recipe_ingredients(recipe_id, sort_order)`); err != nil {
		log.Println("Migration failed - recreating index:", err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Println("Migration failed - commit:", err)
		return
	}

	log.Println("Migration completed: recipe_ingredients.quantity -> REAL, notes added")
}

func migrateRecipeStepsCompleted() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('recipe_steps') WHERE name='completed'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding completed to recipe_steps...")

	_, err = DB.Exec("ALTER TABLE recipe_steps ADD COLUMN completed BOOLEAN DEFAULT FALSE")
	if err != nil {
		log.Println("Migration failed - adding completed to recipe_steps:", err)
		return
	}

	log.Println("Migration completed: recipe_steps.completed added")
}

func migrateListsCoverImage() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('lists') WHERE name='cover_image_path'").Scan(&count)
	if err != nil {
		log.Println("Migration check failed:", err)
		return
	}

	if count > 0 {
		return // Already migrated
	}

	log.Println("Running migration: Adding cover_image_path to lists...")

	_, err = DB.Exec("ALTER TABLE lists ADD COLUMN cover_image_path TEXT DEFAULT NULL")
	if err != nil {
		log.Println("Migration failed - adding cover_image_path to lists:", err)
		return
	}

	log.Println("Migration completed: lists.cover_image_path added")
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}
