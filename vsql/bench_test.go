package vsql_test

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func benchmarkInsert(b *testing.B, db *sql.DB) {
	if _, err := db.Exec("CREATE TABLE foo (g INTEGER, x INTEGER)"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.Exec("INSERT INTO foo (g, x) VALUES (?, ?)", 1, i); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInsertMemory(b *testing.B) {
	db, _ := sql.Open("vsql", ":memory:")
	db.SetMaxOpenConns(1)
	defer db.Close()
	benchmarkInsert(b, db)
}

func BenchmarkInsertFile(b *testing.B) {
	db, _ := sql.Open("vsql", filepath.Join(b.TempDir(), "test.vsql"))
	defer db.Close()
	benchmarkInsert(b, db)
}

func BenchmarkSelectCountFile(b *testing.B) {
	db, _ := sql.Open("vsql", filepath.Join(b.TempDir(), "test.vsql"))
	defer db.Close()
	db.Exec("CREATE TABLE foo (g INTEGER, x INTEGER)")
	for i := 0; i < 200; i++ {
		db.Exec("INSERT INTO foo (g, x) VALUES (?, ?)", 1, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM foo WHERE g = ?", 1).Scan(&n); err != nil {
			b.Fatal(err)
		}
	}
}
