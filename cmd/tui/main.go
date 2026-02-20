package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"film-heatmap/internal/domain"
	"film-heatmap/internal/service"
	store "film-heatmap/internal/store/sqlite"
)

func main() {
	dbPath := os.Getenv("FILM_HEATMAP_DB")
	if dbPath == "" {
		dbPath = "film-heatmap.db"
	}
	st, err := store.Open(dbPath)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	defer st.Close()
	ctx := context.Background()
	logs := service.NewLogService(st)
	heat := service.NewHeatmapService(st)
	statsSvc := service.NewStatsService(st)

	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("\nFilm Heatmap TUI")
		fmt.Println("1) Add log")
		fmt.Println("2) List logs")
		fmt.Println("3) Heatmap")
		fmt.Println("4) Stats")
		fmt.Println("5) Delete log")
		fmt.Println("q) Quit")
		fmt.Print("> ")
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		switch line {
		case "1":
			fmt.Print("Title: ")
			title, _ := r.ReadString('\n')
			title = strings.TrimSpace(title)
			fmt.Print("Date (YYYY-MM-DD, blank=today): ")
			ds, _ := r.ReadString('\n')
			ds = strings.TrimSpace(ds)
			at := time.Now()
			if ds != "" {
				parsed, err := time.ParseInLocation(domain.DateLayout, ds, time.Local)
				if err != nil {
					fmt.Println("invalid date")
					continue
				}
				at = parsed
			}
			if _, err := logs.Add(ctx, domain.AddLogInput{Title: title, LoggedAt: at}); err != nil {
				fmt.Println("error:", err)
			} else {
				fmt.Println("log added")
			}
		case "2":
			rows, err := logs.List(ctx, domain.ListFilter{})
			if err != nil {
				fmt.Println("error:", err)
				continue
			}
			for _, l := range rows {
				fmt.Printf("%s | %s | %s\n", l.ID, l.LocalDate, l.Title)
			}
			fmt.Printf("total: %d\n", len(rows))
		case "3":
			y := time.Now().Year()
			hm, err := heat.Year(ctx, y)
			if err != nil {
				fmt.Println("error:", err)
				continue
			}
			fmt.Printf("Heatmap %d\n", y)
			for day := 0; day < 7; day++ {
				for _, w := range hm.Weeks {
					fmt.Print(charFor(w[day].Intensity), " ")
				}
				fmt.Println()
			}
		case "4":
			y := time.Now().Year()
			s, err := statsSvc.Year(ctx, y)
			if err != nil {
				fmt.Println("error:", err)
				continue
			}
			fmt.Printf("year:%d total:%d active:%d longest:%d current:%d\n", s.Year, s.TotalLogs, s.ActiveDays, s.LongestStreak, s.CurrentStreak)
		case "5":
			fmt.Print("ID: ")
			id, _ := r.ReadString('\n')
			id = strings.TrimSpace(id)
			if err := logs.Delete(ctx, id); err != nil {
				fmt.Println("error:", err)
			} else {
				fmt.Println("deleted")
			}
		case "q", "quit", "exit":
			return
		default:
			if _, err := strconv.Atoi(line); err != nil {
				fmt.Println("unknown command")
			}
		}
	}
}

func charFor(i int) string {
	switch i {
	case 0:
		return "."
	case 1:
		return "l"
	case 2:
		return "m"
	case 3:
		return "h"
	default:
		return "#"
	}
}
