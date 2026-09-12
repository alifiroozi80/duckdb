package duckdb_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alifiroozi80/duckdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Define test structs
type User struct {
	ID    uint `gorm:"primaryKey"`
	Name  string
	Email string `gorm:"unique"`
}

type Product struct {
	ID    uint `gorm:"primaryKey"`
	Name  string
	Price float64
}

type Post struct {
	ID        uint `gorm:"primaryKey"`
	Content   string
	CreatedAt time.Time
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(duckdb.Open(filepath.Join(t.TempDir(), "test.duckdb")), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sqlDB.Close())
	})

	return db
}

// TestMigratorBasicSchema verifies basic schema creation.
func TestMigratorBasicSchema(t *testing.T) {
	db := openTestDB(t)

	// Migrate User table
	require.NoError(t, db.AutoMigrate(&Product{}))

	// Check if table exists
	assert.True(t, db.Migrator().HasTable(&Product{}))
	assert.True(t, db.Migrator().HasColumn(&Product{}, "Price"))
}

// TestMigratorDropTable verifies dropping a table.
func TestMigratorDropTable(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.AutoMigrate(&User{}))
	assert.True(t, db.Migrator().HasTable(&User{}))

	// Drop table and verify
	require.NoError(t, db.Migrator().DropTable(&User{}))
	assert.False(t, db.Migrator().HasTable(&User{}))
}

// TestUniqueConstraint tests that unique constraints are enforced.
func TestUniqueConstraint(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.AutoMigrate(&User{}))
	assert.True(t, db.Migrator().HasColumn(&User{}, "Email"))

	// Create first user
	user1 := User{Name: "User1", Email: "user@example.com"}
	require.NoError(t, db.Create(&user1).Error)

	// Attempt to create a second user with the same email
	user2 := User{Name: "User2", Email: "user@example.com"}
	result := db.Create(&user2)
	assert.Error(t, result.Error, "Expected unique constraint violation")
}

// TestDefaultValues verifies that default values are set correctly.
func TestDefaultValues(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.AutoMigrate(&Post{}))

	// Insert a new post without specifying CreatedAt
	post := Post{Content: "Hello, World!"}
	require.NoError(t, db.Create(&post).Error)

	// Verify CreatedAt has a value (defaulted to the current timestamp)
	assert.NotZero(t, post.CreatedAt)
}

func TestDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.duckdb")

	db, err := gorm.Open(duckdb.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Product{}))
	require.NoError(t, db.Create(&Product{Name: "persisted", Price: 42}).Error)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	reopened, err := gorm.Open(duckdb.Open(path), &gorm.Config{})
	require.NoError(t, err)
	reopenedSQLDB, err := reopened.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, reopenedSQLDB.Close())
	})

	var product Product
	require.NoError(t, reopened.First(&product, "name = ?", "persisted").Error)
	assert.Equal(t, float64(42), product.Price)
}

func TestJSONScanIntoAny(t *testing.T) {
	db := openTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)

	var value any
	require.NoError(t, sqlDB.QueryRow(`SELECT '{"name":"duckdb"}'::JSON`).Scan(&value))
	assert.NotNil(t, value)
}
