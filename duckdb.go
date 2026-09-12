package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

const DriverName = "duckdb"

type Dialector struct {
	*Config
}

type Config struct {
	DriverName string
	DSN        string
	Conn       gorm.ConnPool
}

func Open(dsn string) gorm.Dialector {
	return &Dialector{&Config{DSN: dsn}}
}

func New(config Config) gorm.Dialector {
	return &Dialector{Config: &config}
}

func (dialector Dialector) Name() string {
	return DriverName
}

func (dialector Dialector) Initialize(db *gorm.DB) (err error) {
	if dialector.DriverName == "" {
		dialector.DriverName = DriverName
	}

	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})

	if dialector.Conn != nil {
		db.ConnPool = dialector.Conn
	} else {
		db.ConnPool, err = sql.Open(dialector.DriverName, dialector.Config.DSN)
		if err != nil {
			return err
		}
	}

	var version string
	if err := db.ConnPool.QueryRowContext(context.Background(), "SELECT version()").Scan(&version); err != nil {
		return err
	}

	for k, v := range dialector.ClauseBuilders() {
		db.ClauseBuilders[k] = v
	}
	return
}

func (dialector Dialector) ClauseBuilders() map[string]clause.ClauseBuilder {
	return map[string]clause.ClauseBuilder{
		"INSERT": func(c clause.Clause, builder clause.Builder) {
			if insert, ok := c.Expression.(clause.Insert); ok {
				if stmt, ok := builder.(*gorm.Statement); ok {
					stmt.WriteString("INSERT ")
					if insert.Modifier != "" {
						stmt.WriteString(insert.Modifier)
						stmt.WriteByte(' ')
					}

					stmt.WriteString("INTO ")
					if insert.Table.Name == "" {
						stmt.WriteQuoted(stmt.Table)
					} else {
						stmt.WriteQuoted(insert.Table)
					}
					return
				}
			}

			c.Build(builder)
		},
		"LIMIT": func(c clause.Clause, builder clause.Builder) {
			if limit, ok := c.Expression.(clause.Limit); ok {
				var lmt = -1
				if limit.Limit != nil && *limit.Limit >= 0 {
					lmt = *limit.Limit
				}
				if lmt >= 0 || limit.Offset > 0 {
					builder.WriteString("LIMIT ")
					builder.WriteString(strconv.Itoa(lmt))
				}
				if limit.Offset > 0 {
					builder.WriteString(" OFFSET ")
					builder.WriteString(strconv.Itoa(limit.Offset))
				}
			}
		},
	}
}

func (dialector Dialector) DefaultValueOf(field *schema.Field) clause.Expression {
	return clause.Expr{SQL: "DEFAULT"}
}

func (dialector Dialector) Migrator(db *gorm.DB) gorm.Migrator {
	return Migrator{migrator.Migrator{Config: migrator.Config{
		DB:                          db,
		Dialector:                   dialector,
		CreateIndexAfterCreateTable: true,
	}}}
}

func (dialector Dialector) DataTypeOf(field *schema.Field) string {
	switch field.DataType {
	case schema.Bool:
		return "boolean"
	case schema.Int, schema.Uint:
		size := field.Size
		if field.DataType == schema.Uint {
			size++
		}
		if field.AutoIncrement {
			switch {
			case size <= 16:
				return "smallint"
			case size <= 32:
				return "integer"
			default:
				return "bigint"
			}
		} else {
			switch {
			case size <= 16:
				return "smallint"
			case size <= 32:
				return "integer"
			default:
				return "bigint"
			}
		}
	case schema.Float:
		if field.Precision > 0 {
			if field.Scale > 0 {
				return fmt.Sprintf("numeric(%d, %d)", field.Precision, field.Scale)
			}
			return fmt.Sprintf("numeric(%d)", field.Precision)
		}
		return "decimal"
	case schema.String:
		if field.Size > 0 {
			return fmt.Sprintf("varchar(%d)", field.Size)
		}
		return "text"
	case schema.Time:
		if field.Precision > 0 {
			return fmt.Sprintf("timestamptz(%d)", field.Precision)
		}
		return "timestamptz"
	case schema.Bytes:
		return "blob"
	default:
		if field.Tag.Get("gorm") == "type:jsonb" {
			return "json"
		}
		return dialector.getSchemaCustomType(field)
	}
}

func (dialector Dialector) getSchemaCustomType(field *schema.Field) string {
	sqlType := string(field.DataType)

	if field.AutoIncrement && !strings.Contains(strings.ToLower(sqlType), "integer") {
		size := field.Size
		if field.GORMDataType == schema.Uint {
			size++
		}
		switch {
		case size <= 16:
			sqlType = "smallint"
		case size <= 32:
			sqlType = "integer"
		default:
			sqlType = "bigint"
		}
	}

	return sqlType
}

func (dialector Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
	writer.WriteByte('?')
}

func (dialector Dialector) QuoteTo(writer clause.Writer, str string) {
	var (
		underQuoted, selfQuoted bool
		continuousBacktick      int8
		shiftDelimiter          int8
	)

	for _, v := range []byte(str) {
		switch v {
		case '`':
			continuousBacktick++
			if continuousBacktick == 2 {
				writer.WriteString("")
				continuousBacktick = 0
			}
		case '.':
			if continuousBacktick > 0 || !selfQuoted {
				shiftDelimiter = 0
				underQuoted = false
				continuousBacktick = 0
				writer.WriteString("")
			}
			writer.WriteByte(v)
			continue
		default:
			if shiftDelimiter-continuousBacktick <= 0 && !underQuoted {
				writer.WriteString("")
				underQuoted = true
				if selfQuoted = continuousBacktick > 0; selfQuoted {
					continuousBacktick -= 1
				}
			}

			for ; continuousBacktick > 0; continuousBacktick -= 1 {
				writer.WriteString("")
			}

			writer.WriteByte(v)
		}
		shiftDelimiter++
	}

	if continuousBacktick > 0 && !selfQuoted {
		writer.WriteString("")
	}
	writer.WriteString("")
}

func (dialector Dialector) Explain(sql string, vars ...interface{}) string {
	return logger.ExplainSQL(sql, nil, `"`, vars...)
}

func (dialectopr Dialector) SavePoint(tx *gorm.DB, name string) error {
	tx.Exec("SAVEPOINT " + name)
	return nil
}

func (dialectopr Dialector) RollbackTo(tx *gorm.DB, name string) error {
	tx.Exec("ROLLBACK TO SAVEPOINT " + name)
	return nil
}
