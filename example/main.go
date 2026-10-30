// example is a small program that uses vsql through database/sql:
//
//	go run ./example
//
// It creates a database file (example.vsql) in the current folder, or use
// ":memory:" for a database that isn't saved:
//
//	go run ./example :memory:
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/elliotchance/vsql-go/vsql"
)

func main() {
	name := "example.vsql"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}

	db, err := sql.Open("vsql", name)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Each connection to ":memory:" is a different database.
	db.SetMaxOpenConns(1)

	// Each statement is committed when it finishes.
	_, err = db.Exec(`CREATE TABLE products (
		id INTEGER NOT NULL,
		name VARCHAR(50) NOT NULL,
		price NUMERIC(10, 2),
		added TIMESTAMP(6) WITH TIME ZONE
	)`)
	var e *vsql.Error
	if errors.As(err, &e) && e.SQLState == vsql.DuplicateTableError {
		// The table exists from the last run.
		_, err = db.Exec("DELETE FROM products")
	}
	if err != nil {
		log.Fatal(err)
	}

	// A transaction, with a prepared statement.
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	insert, err := tx.Prepare("INSERT INTO products (id, name, price, added) VALUES (?, ?, ?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	for i, name := range []string{"apple", "banana", "cherry"} {
		price := 1.25 * float64(i+1)
		if _, err := insert.Exec(i+1, name, price, time.Now()); err != nil {
			tx.Rollback()
			log.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}

	rows, err := db.Query("SELECT id, name, price, added FROM products WHERE price > ? ORDER BY id", 2)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	types, err := rows.ColumnTypes()
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range types {
		fmt.Printf("%s %s, ", t.Name(), t.DatabaseTypeName())
	}
	fmt.Println()

	for rows.Next() {
		var id int
		var name string
		// NUMERIC is an exact string, it can be scanned into a decimal type.
		var price string
		var added time.Time
		if err := rows.Scan(&id, &name, &price, &added); err != nil {
			log.Fatal(err)
		}
		fmt.Println(id, name, price, added.Format(time.RFC3339))
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	// Errors have the SQLSTATE.
	_, err = db.Exec("SELECT * FROM nope")
	if errors.As(err, &e) {
		fmt.Println("SQLSTATE", e.SQLState, e.SQLState == vsql.UndefinedTableError, e.Message)
	}
}
