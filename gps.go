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
	"path/filepath"
	"runtime"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// ====================================================================================================================
// Types

type Track struct {
	ID          int             `db:"id"`
	Source      string          `db:"source"`
	Description string          `db:"description"`
	Points      int             `db:"points"`
	Segments    int             `db:"segments"`
	StartTime   string          `db:"start_time"`
	FinishTime  string          `db:"finish_time"`
	TotalTime   float32         `db:"total_time"`
	Region      string          `db:"region"`
	Level       int             `db:"level"`
	LengthMiles float32         `db:"length_miles"`
	MaxSpeed    sql.NullFloat64 `db:"max_speed"`
	AvgSpeed    sql.NullFloat64 `db:"avg_speed"`
	Up          float32         `db:"up"`
	Down        float32         `db:"down"`
	TotalAscent float32         `db:"total_ascent"`
	CategoryID  int             `db:"category_id"`
	SeqNum      int
}

type Page struct {
	Title       string
	Tracks      []Track
	CurrentPage int
	PageSize    int
	TotalTracks int
	TotalPages  int
	OrderBy     string
	Order       string
}

// ====================================================================================================================
// Globals
// map of functions that can be used in the templates, and the templates themselves.
var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"mul": func(a, b int) int { return a * b },
	"mod": func(a, b int) int { return a % b },
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
	"basename": func(path string) string {
		base := filepath.Base(path)
		if i := strings.LastIndexByte(base, '.'); i >= 0 {
			return base[:i]
		}
		return base
	},
	"intRange": func(start, end int) []int {
		var result []int
		for i := start; i <= end; i++ {
			result = append(result, i)
		}
		return result
	},
	"headerLink": func(label, field string, p *Page) template.HTML {
		nextOrder := "ASC"
		arrow := ""
		if p.OrderBy == field {
			if strings.ToUpper(p.Order) == "ASC" {
				nextOrder = "DESC"
				arrow = "▼"
			} else {
				arrow = "▲"
			}
		}
		url := fmt.Sprintf("./?pageSize=%d&amp;Page=%d&amp;order_by=%s&amp;order=%s", p.PageSize, p.CurrentPage, field, nextOrder)
		return template.HTML(fmt.Sprintf(`<a href="%s">%s%s</a>`, url, label, arrow))
	},
}).ParseFiles("html/summary.html"))

// ====================================================================================================================
func renderTemplate(w http.ResponseWriter, tmpl string, p *Page) {
	err := templates.ExecuteTemplate(w, tmpl+".html", p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ====================================================================================================================
func summaryHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	orderBy := r.URL.Query().Get("order_by")
	if orderBy == "" {
		orderBy = "start_time"
	}

	order := r.URL.Query().Get("order")
	if order == "" {
		order = "desc"
	} else if strings.ToLower(order) != "desc" && strings.ToLower(order) != "asc" {
		http.Error(w, "Invalid order parameter. Must be 'asc' or 'desc'.", http.StatusBadRequest)
	}

	qt := r.URL.Query().Get("qt")

	allTracks := readTracks(db, orderBy, order, qt)

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
		for i := range pageTracks {
			pageTracks[i].SeqNum = offset + i + 1
		}
	}

	page := &Page{
		Title:       "Tracks",
		Tracks:      pageTracks,
		CurrentPage: currentPage,
		PageSize:    pageSize,
		TotalTracks: totalTracks,
		TotalPages:  totalPages,
		OrderBy:     orderBy,
		Order:       order,
	}

	renderTemplate(w, "summary", page)
}

// ====================================================================================================================
func parsePositiveInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// ====================================================================================================================
func readTracks(db *sql.DB, orderBy string, order string, qt string) []Track {
	query := `SELECT ID, Source, tracks.Description as Description, Points, Segments, start_time, finish_time, total_time, TrackRegion.description as region, level, length_miles, max_speed, avg_speed, up, down, total_ascent, tracks.category_id 
		FROM tracks, track_legs, TrackRegion 
		WHERE tracks.ID = track_legs.Track_ID and tracks.ID = TrackRegion.Track_ID 
		ORDER BY ` + orderBy + ` ` + order

	stmt, err := db.Prepare(query)
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	tracks := []Track{}
	for rows.Next() {
		track := Track{}
		err = rows.Scan(
			&track.ID,
			&track.Source,
			&track.Description,
			&track.Points,
			&track.Segments,
			&track.StartTime,
			&track.FinishTime,
			&track.TotalTime,
			&track.Region,
			&track.Level,
			&track.LengthMiles,
			&track.MaxSpeed,
			&track.AvgSpeed,
			&track.Up,
			&track.Down,
			&track.TotalAscent,
			&track.CategoryID,
		)
		if err != nil {
			log.Fatal(err)
		}
		tracks = append(tracks, track)
	}
	return tracks
}

// ====================================================================================================================
func openDatabase(configMap map[string]any) *sql.DB {
	cfg := &mysql.Config{
		User:                 configMap["user"].(string),
		Passwd:               configMap["password"].(string),
		Net:                  "tcp",
		Addr:                 configMap["host"].(string) + ":3306", // port is hardcoded to 3306 for mysql , but could be made configurable if needed
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
	if err == nil {
		// defer the closing of our configFile so that we can parse it later on
		defer configFile.Close()
	} else {
		log.Fatal("Error opening config file: ", err)
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
	defer db.Close()

	pingErr := db.Ping()
	if pingErr != nil {
		log.Fatal(pingErr)
	}
	fmt.Println("Connected!")

	fs := http.FileServer(http.Dir("./static"))
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	mux.HandleFunc("/summary/", func(w http.ResponseWriter, r *http.Request) {
		summaryHandler(w, r, db)
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// ====================================================================================================================
