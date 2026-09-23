package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/navyaraksha/imogi/internal/platform/config"
)

const defaultMigrationDirectory = "db/migrations"

const (
	migrationSchema       = "platform"
	migrationVersionTable = migrationSchema + ".goose_db_version"
	legacyVersionTable    = "public.goose_db_version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	migrationDirectory := flags.String("dir", defaultMigrationDirectory, "migration directory")
	if err := flags.Parse(args); err != nil {
		return err
	}

	command := flags.Arg(0)
	if !isMigrationCommand(command) {
		return errors.New("usage: migrate [-dir path] <up|down|status|reset>")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	database, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if err := database.Ping(); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("configure migration dialect: %w", err)
	}
	if err := configureVersionTable(database); err != nil {
		return err
	}

	absoluteMigrationDirectory, err := filepath.Abs(*migrationDirectory)
	if err != nil {
		return fmt.Errorf("resolve migration directory: %w", err)
	}

	switch command {
	case "up":
		err = goose.Up(database, absoluteMigrationDirectory)
	case "down":
		err = goose.Down(database, absoluteMigrationDirectory)
	case "status":
		err = goose.Status(database, absoluteMigrationDirectory)
	case "reset":
		err = goose.Reset(database, absoluteMigrationDirectory)
	}
	if err != nil {
		return fmt.Errorf("migration %s: %w", command, err)
	}
	return nil
}

// configureVersionTable keeps Goose's bookkeeping out of public for new
// databases. The public table is retained as a compatibility path for
// databases that were migrated before the platform schema was introduced.
func configureVersionTable(database *sql.DB) error {
	for _, table := range []string{migrationVersionTable, legacyVersionTable} {
		exists, err := tableExists(database, table)
		if err != nil {
			return fmt.Errorf("check migration version table %q: %w", table, err)
		}
		if exists {
			goose.SetTableName(table)
			return nil
		}
	}

	if _, err := database.Exec("CREATE SCHEMA IF NOT EXISTS " + migrationSchema); err != nil {
		return fmt.Errorf("bootstrap migration schema %q: %w (the database role must be allowed to create schemas)", migrationSchema, err)
	}
	goose.SetTableName(migrationVersionTable)
	return nil
}

func tableExists(database *sql.DB, table string) (bool, error) {
	var exists bool
	if err := database.QueryRow("SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func isMigrationCommand(command string) bool {
	switch command {
	case "up", "down", "status", "reset":
		return true
	default:
		return false
	}
}
