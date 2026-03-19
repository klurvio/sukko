package repository

import "embed"

// Embedded migration files for both PostgreSQL and SQLite.
//
//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS
