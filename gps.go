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
// constants

// query types
const UNALLOCATED = "unallocated"
const REGION = "region"
const TYPE = "type"
const YEAR = "year"
const REGION_AND_TYPE = "regionandtype"
const TRACK_ID_IN = "trackidin"
const DATE_RANGE = "date_range"
const DISTANCE_RANGE = "distance_range"

// parameters for query types
const REGION_PARAM = "region"
const TYPE_PARAM = "type"
const YEAR_PARAM = "year"
const TRACK_ID_IN_PARAM = "ids"
const DATE_RANGE_START_PARAM = "start_date"
const DATE_RANGE_END_PARAM = "end_date"
const DISTANCE_RANGE_MIN_PARAM = "shortest_distance"
const DISTANCE_RANGE_MAX_PARAM = "longest_distance"

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
	Duration    float32         `db:"duration"`
	Region      sql.NullString  `db:"region"`
	Level       sql.NullInt32   `db:"level"`
	LengthMiles float32         `db:"length_miles"`
	MaxSpeed    sql.NullFloat64 `db:"max_speed"`
	AvgSpeed    sql.NullFloat64 `db:"avg_speed"`
	Up          float32         `db:"up"`
	Down        float32         `db:"down"`
	TotalAscent float32         `db:"total_ascent"`
	Type        string          `db:"type"`
	SeqNum      int
}

type Page struct {
	Title           string
	Tracks          []Track
	CurrentPage     int
	PageSize        int
	TotalTracks     int
	TotalPages      int
	OrderBy         string
	Order           string
	QueryType       string
	QueryParameters string
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

		format := "./?pageSize=%d&Page=%d&order_by=%s&order=%s"
		url := fmt.Sprintf(format, p.PageSize, p.CurrentPage, field, nextOrder)
		if p.QueryType != "" {
			url += fmt.Sprintf("&qt=%s", p.QueryType)
			if p.QueryParameters != "" {
				url += fmt.Sprintf("&%s", p.QueryParameters)
			}
		}
		return template.HTML(fmt.Sprintf(`<a href="%s">%s%s</a>`, url, label, arrow))
	},
}).ParseFiles("templates/summary.html"))

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

	var whereClause string
	var qp string
	var desc string
	switch qt {
	case UNALLOCATED:
		typ := r.URL.Query().Get(TYPE_PARAM)
		whereClause = "region is null and type = '" + typ + "'"
		qp = TYPE_PARAM + "=" + typ
		desc = "Not allocated to a region and type = " + typ
	case REGION_AND_TYPE:
		region := r.URL.Query().Get(REGION_PARAM)
		typ := r.URL.Query().Get(TYPE_PARAM)
		whereClause = "region = '" + region + "' and type = '" + typ + "'"
		qp = REGION_PARAM + "=" + region + "&" + TYPE_PARAM + "=" + typ
		desc = "Region = '" + region + "' and with type '" + typ + "'"
	case REGION:
		region := r.URL.Query().Get(REGION_PARAM)
		whereClause = "region = '" + region + "'"
		qp = REGION_PARAM + "=" + region
		desc = "Region = '" + region + "'"
	case TYPE:
		typ := r.URL.Query().Get(TYPE_PARAM)
		whereClause = "type = '" + typ + "'"
		qp = TYPE_PARAM + "=" + typ
		desc = "Type '" + typ + "'"
	case YEAR:
		year := r.URL.Query().Get(YEAR_PARAM)
		typ := r.URL.Query().Get(TYPE_PARAM)
		whereClause = "year(start_time) = " + year + " and type = '" + typ + "'"
		qp = YEAR_PARAM + "=" + year + "&" + TYPE_PARAM + "=" + typ
		desc = "Year " + year + " with type '" + typ + "'"
	case TRACK_ID_IN:
		ids := r.URL.Query().Get(TRACK_ID_IN_PARAM)
		whereClause = "track_id in (" + ids + ")"
		qp = TRACK_ID_IN_PARAM + "=" + ids
		desc = "Tracks with IDs in " + ids
	case DATE_RANGE:
		start_date := r.URL.Query().Get(DATE_RANGE_START_PARAM)
		end_date := r.URL.Query().Get(DATE_RANGE_END_PARAM)
		whereClause = "start_time >= '" + start_date + "' AND start_time <= '" + end_date + "'"
		qp = DATE_RANGE_START_PARAM + "=" + start_date + "&" + DATE_RANGE_END_PARAM + "=" + end_date
		desc = "Tracks between " + start_date + " and " + end_date
	case DISTANCE_RANGE:
		shortest_distance := r.URL.Query().Get(DISTANCE_RANGE_MIN_PARAM)
		longest_distance := r.URL.Query().Get(DISTANCE_RANGE_MAX_PARAM)
		typ := r.URL.Query().Get(TYPE_PARAM)
		if longest_distance == "" {
			whereClause = "length_miles >= (" + shortest_distance + " + 0.0 )"
			qp = DISTANCE_RANGE_MIN_PARAM + "=" + shortest_distance
			desc = "Tracks with length >= " + shortest_distance + " miles"
		} else if shortest_distance == "" {
			whereClause = "length_miles <= (" + longest_distance + " + 0.0 )"
			qp = DISTANCE_RANGE_MAX_PARAM + "=" + longest_distance
			desc = "Tracks with length <= " + longest_distance + " miles"
		} else {
			whereClause = "length_miles >= (" + shortest_distance + " + 0.0 ) AND length_miles <= (" + longest_distance + " + 0.0 )"
			qp = DISTANCE_RANGE_MIN_PARAM + "=" + shortest_distance + "&" + DISTANCE_RANGE_MAX_PARAM + "=" + longest_distance
			desc = "Tracks between " + shortest_distance + " and " + longest_distance + " miles"
		}
		if typ != "" {
			whereClause += " AND type = '" + typ + "'"
			qp += "&" + TYPE_PARAM + "=" + typ
			desc += " with type '" + typ + "'"
		}

	default:
	}
	allTracks := readTracks(db, orderBy, order, whereClause)

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
		Title:           "Tracks",
		Tracks:          pageTracks,
		CurrentPage:     currentPage,
		PageSize:        pageSize,
		TotalTracks:     totalTracks,
		TotalPages:      totalPages,
		OrderBy:         orderBy,
		Order:           order,
		QueryType:       qt,
		QueryParameters: qp,
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
func readTracks(db *sql.DB, orderBy string, order string, whereClause string) []Track {

	fields := []string{
		"track_id",
		"source",
		"description",
		"points",
		"segments",
		"start_time",
		"finish_time",
		"duration",
		"region",
		"level",
		"length_miles",
		"max_speed",
		"avg_speed",
		"up",
		"down",
		"total_ascent",
		"type"}

	fieldList := strings.Join(fields, ", ")
	query := "SELECT " + fieldList + " FROM Summary"

	if whereClause != "" {
		query += " WHERE " + whereClause
	}
	query += " ORDER BY " + orderBy + " " + order

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
			&track.Duration,
			&track.Region,
			&track.Level,
			&track.LengthMiles,
			&track.MaxSpeed,
			&track.AvgSpeed,
			&track.Up,
			&track.Down,
			&track.TotalAscent,
			&track.Type,
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
