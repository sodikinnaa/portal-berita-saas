package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	db, err := pgxpool.New(context.Background(), "postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	rows, _ := db.Query(context.Background(), "SELECT TO_CHAR(created_at, 'YYYY-MM-DD') AS label, COUNT(*) AS val FROM articles WHERE created_at >= NOW() - INTERVAL '7 days' GROUP BY label ORDER BY label ASC")
	fmt.Println("Chart data:")
	for rows.Next() {
		var label string
		var val int
		rows.Scan(&label, &val)
		fmt.Printf("%s: %d\n", label, val)
	}
	rows.Close()

	rows2, _ := db.Query(context.Background(), "SELECT created_at FROM articles")
	today := 0
	now := time.Now()
	y2, m2, d2 := now.Date()
	for rows2.Next() {
		var ca time.Time
		rows2.Scan(&ca)
		y1, m1, d1 := ca.Date()
		if y1 == y2 && m1 == m2 && d1 == d2 {
			today++
		}
	}
	rows2.Close()
	fmt.Printf("Today count in Go: %d\n", today)
}
