package duckdb

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

var ErrDuckDBNotSupported = errors.New("DuckDB are not supported this operation")

var typeAliasMap = map[string][]string{
	"int":                      {"integer"},
	"int2":                     {"smallint"},
	"int4":                     {"integer"},
	"int8":                     {"bigint"},
	"smallint":                 {"int2"},
	"integer":                  {"int4"},
	"bigint":                   {"int8"},
	"decimal":                  {"numeric"},
	"numeric":                  {"decimal"},
	"timestamptz":              {"timestamp with time zone"},
	"timestamp with time zone": {"timestamptz"},
	"bool":                     {"boolean"},
	"boolean":                  {"bool"},
	"bit":                      {"bitstring"},
	"char":                     {"character"},
	"varchar":                  {"character varying"},
	"float4":                   {"real"},
	"float8":                   {"double"},
	"blob":                     {"binary"},
}

type Migrator struct {
	migrator.Migrator
}

type BuildIndexOptionsInterface interface {
	BuildIndexOptions([]schema.IndexOption, *gorm.Statement) []interface{}
}

// Database

func (m Migrator) CurrentDatabase() (name string) {
	m.DB.Raw("SELECT CURRENT_DATABASE()").Scan(&name)
	return
}

func (m Migrator) FullDataTypeOf(field *schema.Field) clause.Expr {
	expr := m.Migrator.FullDataTypeOf(field)

	if value, ok := field.TagSettings["COMMENT"]; ok {
		expr.SQL += " COMMENT " + m.Dialector.Explain("?", value)
	}

	return expr
}

func (m Migrator) GetTypeAliases(databaseTypeName string) []string {
	return typeAliasMap[databaseTypeName]
}

// Tables

func (m Migrator) createSequence(values ...interface{}) error {
	for _, value := range m.ReorderModels(values, false) {
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
			if stmt.Schema != nil {
				for range stmt.Schema.DBNames {
					if execErr := m.DB.Exec(
						"CREATE SEQUENCE IF NOT EXISTS ?",
						m.CurrentTable(stmt)).Error; execErr != nil {
						return execErr
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m Migrator) CreateTable(values ...interface{}) (err error) {

	if err := m.createSequence(values...); err != nil {
		return err
	}

	for _, value := range m.ReorderModels(values, false) {
		tx := m.DB.Session(&gorm.Session{})
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) (err error) {

			if stmt.Schema == nil {
				return errors.New("failed to get schema")
			}

			var (
				createTableSQL          = "CREATE TABLE ? ("
				values                  = []interface{}{m.CurrentTable(stmt)}
				hasPrimaryKeyInDataType bool
			)

			for _, dbName := range stmt.Schema.DBNames {
				field := stmt.Schema.FieldsByDBName[dbName]
				if !field.IgnoreMigration {
					if dbName == "id" {
						// s := fmt.Sprintf("{%s  %!s(bool=false)}", m.CurrentTable(stmt).(clause.Table).Name)
						s := "{" + m.CurrentTable(stmt).(clause.Table).Name + "  %!s(bool=false)}"
						re := regexp.MustCompile(`\{([^ ]+)`)
						match := re.FindStringSubmatch(s)
						pk := fmt.Sprintf("? ? DEFAULT nextval('%s')", match[1])
						createTableSQL += pk

					} else {
						createTableSQL += "? ?"
					}
					hasPrimaryKeyInDataType = hasPrimaryKeyInDataType || strings.Contains(strings.ToUpper(m.DataTypeOf(field)), "PRIMARY KEY")
					values = append(values, clause.Column{Name: dbName}, m.DB.Migrator().FullDataTypeOf(field))
					createTableSQL += ","
				}
			}

			if !hasPrimaryKeyInDataType && len(stmt.Schema.PrimaryFields) > 0 {
				createTableSQL += "PRIMARY KEY ?,"
				primaryKeys := make([]interface{}, 0, len(stmt.Schema.PrimaryFields))
				for _, field := range stmt.Schema.PrimaryFields {
					primaryKeys = append(primaryKeys, clause.Column{Name: field.DBName})
				}

				values = append(values, primaryKeys)
			}

			for _, idx := range stmt.Schema.ParseIndexes() {
				if m.CreateIndexAfterCreateTable {
					defer func(value interface{}, name string) {
						if err == nil {
							err = tx.Migrator().CreateIndex(value, name)
						}
					}(value, idx.Name)
				} else {
					if idx.Class != "" {
						createTableSQL += idx.Class + " "
					}
					createTableSQL += "INDEX ? ?"

					if idx.Comment != "" {
						createTableSQL += fmt.Sprintf(" COMMENT '%s'", idx.Comment)
					}

					if idx.Option != "" {
						createTableSQL += " " + idx.Option
					}

					createTableSQL += ","
					values = append(values, clause.Column{Name: idx.Name}, tx.Migrator().(BuildIndexOptionsInterface).BuildIndexOptions(idx.Fields, stmt))
				}
			}

			if !m.DB.DisableForeignKeyConstraintWhenMigrating && !m.DB.IgnoreRelationshipsWhenMigrating {
				for _, rel := range stmt.Schema.Relationships.Relations {
					if rel.Field.IgnoreMigration {
						continue
					}
					if constraint := rel.ParseConstraint(); constraint != nil {
						if constraint.Schema == stmt.Schema {
							sql, vars := constraint.Build()
							createTableSQL += sql + ","
							values = append(values, vars...)
						}
					}
				}
			}

			for _, uni := range stmt.Schema.ParseUniqueConstraints() {
				createTableSQL += "CONSTRAINT ? UNIQUE (?),"
				values = append(values, clause.Column{Name: uni.Name}, clause.Expr{SQL: stmt.Quote(uni.Field.DBName)})
			}

			for _, chk := range stmt.Schema.ParseCheckConstraints() {
				createTableSQL += "CONSTRAINT ? CHECK (?),"
				values = append(values, clause.Column{Name: chk.Name}, clause.Expr{SQL: chk.Constraint})
			}

			createTableSQL = strings.TrimSuffix(createTableSQL, ",")

			createTableSQL += ")"

			if tableOption, ok := m.DB.Get("gorm:table_options"); ok {
				createTableSQL += fmt.Sprint(tableOption)
			}

			err = tx.Exec(createTableSQL, values...).Error
			return err
		}); err != nil {
			return err
		}
	}
	for _, value := range m.ReorderModels(values, false) {
		if err = m.RunWithValue(value, func(stmt *gorm.Statement) error {
			if stmt.Schema != nil {
				for _, fieldName := range stmt.Schema.DBNames {
					field := stmt.Schema.FieldsByDBName[fieldName]
					if field.Comment != "" {
						if err := m.DB.Exec(
							"COMMENT ON COLUMN ?.? IS ?",
							m.CurrentTable(stmt), clause.Column{Name: field.DBName}, gorm.Expr(m.Migrator.Dialector.Explain("$1", field.Comment)),
						).Error; err != nil {
							return err
						}
					}
				}
			}
			return nil
		}); err != nil {
			return
		}
	}
	return
}

func (m Migrator) DropTable(values ...interface{}) error {
	values = m.ReorderModels(values, false)
	tx := m.DB.Session(&gorm.Session{})
	for i := len(values) - 1; i >= 0; i-- {
		if err := m.RunWithValue(values[i], func(stmt *gorm.Statement) error {
			return tx.Exec("DROP TABLE IF EXISTS ? CASCADE", m.CurrentTable(stmt)).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m Migrator) CurrentSchema(stmt *gorm.Statement, table string) (interface{}, interface{}) {
	if strings.Contains(table, ".") {
		if tables := strings.Split(table, `.`); len(tables) == 2 {
			return tables[0], tables[1]
		}
	}

	if stmt.TableExpr != nil {
		if tables := strings.Split(stmt.TableExpr.SQL, `"."`); len(tables) == 2 {
			return strings.TrimPrefix(tables[0], `"`), table
		}
	}
	return clause.Expr{SQL: "CURRENT_SCHEMA()"}, table
}

func (m Migrator) HasTable(value interface{}) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		currentSchema, curTable := m.CurrentSchema(stmt, stmt.Table)
		return m.DB.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ? AND table_type = ?", currentSchema, curTable, "BASE TABLE").Scan(&count).Error
	})
	return count > 0
}

func (m Migrator) RenameTable(oldName, newName interface{}) (err error) {
	resolveTable := func(name interface{}) (result string, err error) {
		if v, ok := name.(string); ok {
			result = v
		} else {
			stmt := &gorm.Statement{DB: m.DB}
			if err = stmt.Parse(name); err == nil {
				result = stmt.Table
			}
		}
		return
	}

	var oldTable, newTable string

	if oldTable, err = resolveTable(oldName); err != nil {
		return
	}

	if newTable, err = resolveTable(newName); err != nil {
		return
	}

	if !m.HasTable(oldTable) {
		return
	}

	return m.DB.Exec("RENAME TABLE ? TO ?",
		clause.Table{Name: oldTable},
		clause.Table{Name: newTable},
	).Error
}

func (m Migrator) GetTables() (tableList []string, err error) {
	currentSchema, _ := m.CurrentSchema(m.DB.Statement, "")
	return tableList, m.DB.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = ? AND table_type = ?", currentSchema, "BASE TABLE").Scan(&tableList).Error
}

// Columns
func (m Migrator) DropColumn(dst interface{}, field string) error {
	if err := m.Migrator.DropColumn(dst, field); err != nil {
		return err
	}

	m.resetPreparedStmts()
	return nil
}

// should reset prepared stmts when table changed
// https://duckdb.org/docs/sql/query_syntax/prepared_statements.html
func (m Migrator) resetPreparedStmts() {
	if m.DB.PrepareStmt {
		if pdb, ok := m.DB.ConnPool.(*gorm.PreparedStmtDB); ok {
			pdb.Reset()
		}
	}
}

func (m Migrator) MigrateColumn(value interface{}, field *schema.Field, columnType gorm.ColumnType) error {
	// skip primary field
	if !field.PrimaryKey {
		if err := m.Migrator.MigrateColumn(value, field, columnType); err != nil {
			return err
		}
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		var description string
		currentSchema, curTable := m.CurrentSchema(stmt, stmt.Table)
		values := []interface{}{currentSchema, curTable, field.DBName, stmt.Table, currentSchema}
		checkSQL := "SELECT description FROM pg_catalog.pg_description "
		checkSQL += "WHERE objsubid = (SELECT ordinal_position FROM information_schema.columns WHERE table_schema = ? AND table_name = ? AND column_name = ?) "
		checkSQL += "AND objoid = (SELECT oid FROM pg_catalog.pg_class WHERE relname = ? AND relnamespace = "
		checkSQL += "(SELECT oid FROM pg_catalog.pg_namespace WHERE nspname = ?))"
		m.DB.Raw(checkSQL, values...).Row().Scan(&description)

		comment := strings.Trim(field.Comment, "'")
		comment = strings.Trim(comment, `"`)
		if field.Comment != "" && comment != description {
			if err := m.DB.Exec(
				"COMMENT ON COLUMN ?.? IS ?",
				m.CurrentTable(stmt), clause.Column{Name: field.DBName}, gorm.Expr(m.Migrator.Dialector.Explain("$1", field.Comment)),
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (m Migrator) HasColumn(value interface{}, field string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		name := field
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(field); field != nil {
				name = field.DBName
			}
		}

		currentSchema, curTable := m.CurrentSchema(stmt, stmt.Table)
		return m.DB.Raw(
			"SELECT count(*) FROM INFORMATION_SCHEMA.columns WHERE table_schema = ? AND table_name = ? AND column_name = ?",
			currentSchema, curTable, name,
		).Scan(&count).Error
	})

	return count > 0
}

func (m Migrator) RenameColumn(dst interface{}, oldName, field string) error {
	if err := m.Migrator.RenameColumn(dst, oldName, field); err != nil {
		return err
	}

	m.resetPreparedStmts()
	return nil
}

// TODO: Implement below function.
// func (m Migrator) ColumnTypes(value interface{}) (columnTypes []gorm.ColumnType, err error)

// Views
func (m Migrator) CreateView(name string, option gorm.ViewOption) error {
	return ErrDuckDBNotSupported
}

func (m Migrator) DropView(name string) error {
	return ErrDuckDBNotSupported
}

// Constraints

// WARNING: Constraints have a strong impact on performance:
// they slow down loading and updates but speed up certain queries.
// https://duckdb.org/docs/guides/performance/schema.html#constraints

func (m Migrator) HasConstraint(value interface{}, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint, table := m.GuessConstraintInterfaceAndTable(stmt, name)
		if constraint != nil {
			name = constraint.GetName()
		}
		currentSchema, curTable := m.CurrentSchema(stmt, table)

		return m.DB.Raw(
			"SELECT count(*) FROM INFORMATION_SCHEMA.table_constraints WHERE table_schema = ? AND table_name = ? AND constraint_name = ?",
			currentSchema, curTable, name,
		).Scan(&count).Error
	})

	return count > 0
}

// https://duckdb.org/docs/sql/statements/alter_table.html#add--drop-constraint
func (m Migrator) DropConstraint(dst interface{}, name string) error {
	return ErrDuckDBNotSupported
}

// TODO: Implement below function.
// func (m Migrator) CreateConstraint(value interface{}, name string) error {}

// Indexes

func (m Migrator) CreateIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if idx := stmt.Schema.LookIndex(name); idx != nil {
				opts := m.BuildIndexOptions(idx.Fields, stmt)
				values := []interface{}{clause.Column{Name: idx.Name}, m.CurrentTable(stmt), opts}

				createIndexSQL := "CREATE "
				if idx.Class != "" {
					createIndexSQL += idx.Class + " "
				}
				createIndexSQL += "INDEX IF NOT EXISTS ? ON ?"

				if idx.Type != "" {
					createIndexSQL += " USING " + idx.Type + "(?)"
				} else {
					createIndexSQL += " ?"
				}

				err := m.DB.Exec(createIndexSQL, values...).Error
				if err != nil {
					return err
				}

				if !m.HasIndex(value, name) {
					return fmt.Errorf("failed to create index with name %v", name)
				}
				return nil
			}
		}

		return fmt.Errorf("failed to create index with name %v", name)
	})
}

func (m Migrator) DropIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if idx := stmt.Schema.LookIndex(name); idx != nil {
				name = idx.Name
			}
		}

		return m.DB.Exec("DROP INDEX IF EXISTS ?", clause.Column{Name: name}).Error
	})
}

func (m Migrator) RenameIndex(dst interface{}, oldName, newName string) error {
	return ErrDuckDBNotSupported
}

func (m Migrator) HasIndex(value interface{}, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if idx := stmt.Schema.LookIndex(name); idx != nil {
				name = idx.Name
			}
		}
		currentSchema, curTable := m.CurrentSchema(stmt, stmt.Table)
		return m.DB.Raw(
			"SELECT count(*) FROM pg_indexes WHERE tablename = ? AND indexname = ? AND schemaname = ?", curTable, name, currentSchema,
		).Scan(&count).Error
	})

	return count > 0
}
