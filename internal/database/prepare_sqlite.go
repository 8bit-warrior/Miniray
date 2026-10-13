package database

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func PrepareSQLiteForMigration(dbPath string) error {
	gdb, err := gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=10000"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	for _, table := range []string{"users", "settings", "inbounds"} {
		if !sqliteTableExists(sqlDB, table) {
			return fmt.Errorf("not a miniray panel database: required table %q is missing", table)
		}
	}
	for _, model := range allModels() {
		if err := gdb.AutoMigrate(model); err != nil && !isIgnorableDuplicateColumnErr(gdb, err, model) {
			return fmt.Errorf("upgrade panel schema for %T: %w", model, err)
		}
	}
	return nil
}
