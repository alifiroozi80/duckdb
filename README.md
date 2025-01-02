<!-- PROJECT LOGO -->
<br />
<div align="center">
  <picture>
    <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/duckdb/duckdb/main/logo/DuckDB_Logo-horizontal-dark-mode.svg">
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/duckdb/duckdb/main/logo/DuckDB_Logo-horizontal-dark-mode.svg">
    <img alt="DuckDB logo" src="https://raw.githubusercontent.com/duckdb/duckdb/main/logo/DuckDB_Logo-horizontal-dark-mode.svg" height="100">
  </picture>

<h3 align="center">GORM DuckDB Driver</h3>
	
	[![Go Reference](https://pkg.go.dev/badge/github.com/alifiroozi80/duckdb.svg)](https://pkg.go.dev/github.com/alifiroozi80/duckdb)
	[![GitHub go.mod Go version (subdirectory of monorepo)](https://img.shields.io/github/go-mod/go-version/alifiroozi80/duckdb?logo=go)](https://img.shields.io/github/go-mod/go-version/alifiroozi80/duckdb?logo=go)
	[![GitHub release (latest by date)](https://img.shields.io/github/v/release/alifiroozi80/duckdb)](https://img.shields.io/github/v/release/alifiroozi80/duckdb)
	[![Go Report Card](https://goreportcard.com/badge/github.com/alifiroozi80/duckdb)](https://goreportcard.com/report/github.com/alifiroozi80/duckdb)
	[![GitHub License](https://img.shields.io/github/license/alifiroozi80/duckdb?&color=blue)](https://img.shields.io/github/license/alifiroozi80/duckdb?&color=blue)

  <p align="center">
    <a href="https://github.com/alifiroozi80/duckdb/issues">Report Bug</a>
    <!-- · -->
    <!-- <a href="https://github.com/alifiroozi80/duckdb/issues">Request Feature</a> -->
  </p>
</div>

---

## Quick Start

```go
import (
  "github.com/alifiroozi80/duckdb"
  "gorm.io/gorm"
)

// DO NOT use 'gorm.Model' here. See 'Limitations' for more.
type Product struct {
	ID        uint `gorm:"primarykey"`
	Code      string
	Price     uint
	CreatedAt time.Time
	UpdatedAt time.Time
}

func main() {
	// duckdb extentions: .ddb, .duckdb, .db
        db, err := gorm.Open(duckdb.Open("duckdb.ddb"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	// Migrate the schema
	db.AutoMigrate(&Product{})

	// Create
	db.Create(&Product{Code: "D42", Price: 100})

        // Read
	var product Product
	db.First(&product, 1)                 // find product with integer primary key
	db.First(&product, "code = ?", "D42") // find product with code D42

	// Update - update product's price to 200
	db.Model(&product).Update("Price", 200)
	// Update - update multiple fields
	db.Model(&product).Updates(Product{Price: 200, Code: "F42"}) // non-zero fields
	db.Model(&product).Updates(map[string]interface{}{"Price": 200, "Code": "F42"})

	// Delete - delete product
	db.Delete(&product, 1)
}
```

Checkout [https://gorm.io](https://gorm.io) for details.


## Limitations

#### `deleted_at` field.

DuckDB has two index types:

- Min-Max (Zonemap)
- [Adaptive Radix Tree](https://db.in.tum.de/~leis/papers/ART.pdf)
In DuckDB, ART indexes are automatically created for columns with a `UNIQUE` or `PRIMARY KEY` constraint and can be defined using `CREATE INDEX`.

However, every technology has its pros and cons. Despite being helpful (read attached pdf), ART indexes have some limitations.

ART indexes create a secondary copy of the data in a second location – this complicates processing, particularly when combined with transactions. Certain limitations apply when it comes to modifying data stored in secondary indexes.

Due to the presence of transactions, data can only be removed from the index after the transaction that performed the delete is committed and no further transactions that refer to the old entry are still present in the index. As a result, transactions that perform deletions followed by insertions may trigger unexpected, unique constraint violations, as the deleted tuple has not yet been removed from the index. For example:

GORM will update the `deleted_at` column when you perform a `db.Delete()`. so your `db.Delete()` will be translate to:

```sql
UPDATE products SET deleted_at='2024-11-01 12:06:00.942' WHERE products.id = 1 AND products.deleted_at IS NULL;
```

And it cause an error:

```bash
Constraint Error: Duplicate key "id: 1" violates primary key constraint. If this is an unexpected constraint violation please double check with the known index limitations section in our documentation (https://duckdb.org/docs/sql/indexes).
```

That is why you should not use the default `gorm.Model` structure and manually use `ID`, `CreatedAt` and `UpdatedAt`.

For more info, see [DuckDB documentations](https://duckdb.org/docs/sql/constraints#primary-key-and-unique-constraint).

---

#### JSONB

DuckDB doesn't support JSONB schema yet, so whenever you use `gorm:"type:jsonb"`, it will translate it to `gorm:"type:json"` under the hood.

Why did I add this?

Because in a couple of projects that I was contributing to, this type was being used in GORM drivers, so I added this small thing to avoid facing the below error:

```bash
Catalogue Error: Type with name jsonb does not exist!
```

<!-- CONTRIBUTING -->

## Contributing

Any contributions you make are **greatly appreciated**.

See [here](https://github.com/alifiroozi80/duckdb/blob/main/CONTRIBUTING.md) for more details on contributing.

### Roadmap

- [ ] Support switch indexes between ART and Min-Max in the configuraion.
- [ ] Implement TODO functions:
	- ColumnTypes
	- CreateConstraint
- [ ] Support `sequence` for each field that needs auto-increment (See [here](https://github.com/alifiroozi80/duckdb/issues/1)).

<!-- LICENSE -->

## License

The license is under the MIT License. See [LICENSE](https://github.com/alifiroozi80/duckdb/blob/main/LICENSE) for more
information.

## ❤ Show your support

Give a ⭐️ if this project helped you!
