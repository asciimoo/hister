// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPLv3+

package model

import (
	"fmt"
	"time"

	"github.com/asciimoo/hister/config"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	//	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
)

//// DBTypedef represents the type of database being used.
//type DBTypedef int
//
//const (
//	// Sqlite represents SQLite database type.
//	Sqlite DBTypedef = iota
//	// Psql represents PostgreSQL database type.
//	Psql
//)
//
//// ErrDBType is returned when an unknown database type is encountered.
//const ErrDBType = errors.New("Unknown database type")
//// DBType holds the type of the database being used.
//var DBType = Sqlite

// DB is the global database instance.
var DB *gorm.DB

// Init initializes the database connection and runs migrations.
func Init(c *config.Config) error {
	dbCfg := &gorm.Config{}
	if c.App.DebugSQL {
		dbCfg.Logger = logger.Default.LogMode(logger.Info)
	} else {
		dbCfg.Logger = logger.Default.LogMode(logger.Silent)
	}
	var err error
	DB, err = gorm.Open(sqlite.Open(c.DatabaseConnection()), dbCfg)
	if err != nil {
		return err
	}
	//switch c.DB.Type {
	//case "postgresql", "postgres", "psql":
	//	DB, err = gorm.Open(postgres.Open(c.DB.Connection), dbCfg)
	//	if err != nil {
	//		return err
	//	}
	//	DBType = Psql
	//default:
	//	return ErrDBType
	//}
	err = migrate()
	if err != nil {
		return fmt.Errorf("custom migration of database '%s' has failed: %w", c.DatabaseConnection(), err)
	}
	err = automigrate()
	if err != nil {
		return fmt.Errorf("auto migration of database '%s' has failed: %w", c.DatabaseConnection(), err)
	}
	err = DB.SetupJoinTable(&History{}, "Links", &HistoryLink{})
	if err != nil {
		return fmt.Errorf("failed to setup join table for URL history: %w", err)
	}
	return nil
}

func automigrate() error {
	return DB.AutoMigrate(
		&Database{},
		&History{},
		&Link{},
		&HistoryLink{},
	)
}

// Database represents the database version tracking table.
type Database struct {
	ID      uint `gorm:"primaryKey"`
	Version uint
}

// CommonFields contains fields common to all models.
type CommonFields struct {
	ID        uint       `gorm:"primary_key" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}
