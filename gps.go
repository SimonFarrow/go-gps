package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"github.com/go-sql-driver/mysql"
)

// ====================================================================================================================
// Types

type Track struct {
	ID          int    `db:"id"`
	Source      string `db:"source"`
	Description string `db:"description"`
	CategoryID  string `db:"category_id"`
}

type Page struct {
	Title       string
	Tracks      []Track
	CurrentPage int
	PageSize    int
	TotalTracks int
	TotalPages  int
}

// ====================================================================================================================
// Globals
var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"mul": func(a, b int) int { return a * b },
	"min": func(a, b int) int {
		if a < b {
			return a
		}
		return b
	},
	"max": func(a, b int) int {
		if a > b {
			return a
		}
		return b
	},
	"intRange": func(start, end int) []int {
		var result []int
		for i := start; i <= end; i++ {
			result = append(result, i)
		}
		return result
	},
}).ParseFiles("html/view.html"))
var db *sql.DB
var validPath = regexp.MustCompile("^/(view)/([a-zA-Z0-9]+)$")

// ====================================================================================================================
func renderTemplate(w http.ResponseWriter, tmpl string, p *Page) {
	err := templates.ExecuteTemplate(w, tmpl+".html", p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ====================================================================================================================
func viewHandler(w http.ResponseWriter, r *http.Request, allTracks []Track) {
	// Parse query parameters
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")

	// Set defaults
	currentPage := 1
	pageSize := 50

	// Parse page number
	if pageStr != "" {
		if p, err := parsePositiveInt(pageStr); err == nil && p > 0 {
			currentPage = p
		}
	}

	// Parse page size
	if pageSizeStr != "" {
		if ps, err := parsePositiveInt(pageSizeStr); err == nil && ps > 0 && ps <= 1000 {
			pageSize = ps
		}
	}

	totalTracks := len(allTracks)
	totalPages := (totalTracks + pageSize - 1) / pageSize

	// Validate page number
	if currentPage > totalPages && totalPages > 0 {
		currentPage = totalPages
	}

	// Calculate offset
	offset := (currentPage - 1) * pageSize
	end := offset + pageSize
	if end > totalTracks {
		end = totalTracks
	}

	// Get tracks for current page
	var pageTracks []Track
	if offset < totalTracks {
		pageTracks = allTracks[offset:end]
	}

	page := &Page{
		Title:       "Tracks",
		Tracks:      pageTracks,
		CurrentPage: currentPage,
		PageSize:    pageSize,
		TotalTracks: totalTracks,
		TotalPages:  totalPages,
	}

	renderTemplate(w, "view", page)
}

func parsePositiveInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// ====================================================================================================================
func main() {
	// Open our jsonFile
	jsonFileStr := "/Users/simonf/Documents/GPS/server/cgi-bin/gps_config.json"
	if runtime.GOOS == "windows" {
		jsonFileStr = "C:" + jsonFileStr
	} else {
		jsonFileStr = "/mnt/c" + jsonFileStr
	}

	jsonFile, err := os.Open(jsonFileStr)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println("Successfully Opened file")
		// defer the closing of our jsonFile so that we can parse it later on
		defer jsonFile.Close()
	}

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	var result map[string]interface{}
	err = json.Unmarshal([]byte(byteValue), &result)
	if err != nil {
		fmt.Println("Error unmarshalling JSON:", err)
		return
	}

	jdb := result["database"].(map[string]any)
	fmt.Println(jdb["dbtype"])

	// Capture connection properties.
	cfg := &mysql.Config{
		User:                 jdb["user"].(string),
		Passwd:               jdb["password"].(string),
		Net:                  "tcp",
		Addr:                 jdb["host"].(string) + ":3306",
		DBName:               jdb["schema"].(string),
		AllowNativePasswords: true, // AllowNativePasswords is required when the MySQL user account has a password that is stored using the native password hashing method.
	}

	// Get a database handle. NB not MariaDB as per the config
	db, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatal(err)
	}

	pingErr := db.Ping()
	if pingErr != nil {
		log.Fatal(pingErr)
	}
	fmt.Println("Connected!")

	rows, err := db.Query("SELECT * FROM tracks")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	track := Track{}
	tracks := []Track{}
	for rows.Next() {
		err = rows.Scan(&track.ID, &track.Source, &track.Description, &track.CategoryID)
		if err != nil {
			log.Fatal(err)
		}
		tracks = append(tracks, track)
		// fmt.Printf("ID: %d, Source: %s, Description: %s, Category ID: %s\n", track.ID, track.Source, track.Description, track.CategoryID)
	}

	http.HandleFunc("/view/", func(w http.ResponseWriter, r *http.Request) {
		viewHandler(w, r, tracks)
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// ====================================================================================================================
