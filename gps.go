package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime"
)

// ====================================================================================================================
// Types

type Track struct {
	ID          int    `db:"id"`
	Source      string `db:"source"`
	Description string `db:"description"`
	Category_ID string `db:"category_id"`
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
	pageSize := 25

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

// ====================================================================================================================
func parsePositiveInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// ====================================================================================================================
func main() {

	var configFileStr string
	if len(os.Args) > 1 {
		configFileStr = os.Args[1]
	} else {
		// Open our default jsonFile
		configFileStr = "/Users/simonf/Documents/GPS/server/cgi-bin/gps_config.json"
		if runtime.GOOS == "windows" {
			configFileStr = "C:" + configFileStr
		} else {
			configFileStr = "/mnt/c" + configFileStr
		}
	}

	configMap := readConfigFile(configFileStr)
	databaseInfo := configMap["database"].(map[string]any)
	fmt.Println(databaseInfo["dbtype"])
	db := openDatabase(databaseInfo)

	pingErr := db.Ping()
	if pingErr != nil {
		log.Fatal(pingErr)
	}
	fmt.Println("Connected!")

	fs := http.FileServer(http.Dir("./static"))
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	tracks := readTracks(db)

	mux.HandleFunc("/view/", func(w http.ResponseWriter, r *http.Request) {
		viewHandler(w, r, tracks)
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// ====================================================================================================================
func readTracks(db *sql.DB) []Track {
	rows, err := db.Query("SELECT ID, Source, Description, Category_ID FROM tracks")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	track := Track{}
	tracks := []Track{}
	for rows.Next() {
		err = rows.Scan(&track.ID, &track.Source, &track.Description, &track.Category_ID)
		if err != nil {
			log.Fatal(err)
		}
		tracks = append(tracks, track)
		// fmt.Printf("ID: %d, Source: %s, Description: %s, Category ID: %s\n", track.ID, track.Source, track.Description, track.CategoryID)
	}
	return tracks
}

// ====================================================================================================================
func openDatabase(configMap map[string]any) *sql.DB {
	cfg := &mysql.Config{
		User:                 configMap["user"].(string),
		Passwd:               configMap["password"].(string),
		Net:                  "tcp",
		Addr:                 configMap["host"].(string) + ":3306",
		DBName:               configMap["schema"].(string),
		AllowNativePasswords: true, // AllowNativePasswords is required when the MySQL user account has a password that is stored using the native password hashing method.
	}

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatal("Error opening database: ", err)
	}
	return db
}

// ====================================================================================================================
func readConfigFile(configFileStr string) map[string]interface{} {
	configFile, err := os.Open(configFileStr)
	if err != nil {
		log.Fatal("Error opening config file: ", err)
	} else {
		fmt.Println("Successfully Opened file")
		// defer the closing of our configFile so that we can parse it later on
		defer configFile.Close()
	}

	byteValue, err := io.ReadAll(configFile)
	if err != nil {
		log.Fatal("Error reading file:", err)
	}

	var configMap map[string]interface{}
	err = json.Unmarshal([]byte(byteValue), &configMap)
	if err != nil {
		log.Fatal("Error unmarshalling JSON:", err)
	}
	return configMap
}

// ====================================================================================================================
