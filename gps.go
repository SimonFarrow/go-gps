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
	"slices"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// Constants
// ===

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

// FIELDS is the list of fields in the Summary table, and is used to construct the SQL query and to validate the order_by parameter.
// cant be declared const for dogmatic go reasons, but should be treated as a constant.
var FIELDS = []string{
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

// Types
// ===

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
	DropList        []string
}

// Globals
// ===
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
				arrow = "▲"
			} else {
				arrow = "▼"
			}
		}

		url := fmt.Sprintf("./?pageSize=%d&Page=%d%s", p.PageSize, p.CurrentPage, pageLink(p, field, nextOrder))
		return template.HTML(fmt.Sprintf(`<a href="%s">%s%s</a>`, url, label, arrow))
	},
	"pageLink": func(p *Page) template.URL {
		return pageLink(p, p.OrderBy, p.Order)
	},
	"columnVisibility": func(dropList []string, name string) template.CSS {
		if slices.Contains(dropList, name) {
			return template.CSS("visibility: collapse")
		}
		return template.CSS("visibility: visible")
	},
}).ParseFiles("templates/summary.html"))

// pageLink
// ===
func pageLink(p *Page, order_by, order string) template.URL {
	url := "&order_by=" + order_by + "&order=" + order
	if p.QueryType != "" {
		url += "&qt=" + p.QueryType
		if p.QueryParameters != "" {
			url += "&" + p.QueryParameters
		}
	}
	return template.URL(url)
}

// renderTemplate
// ===
func renderTemplate(w http.ResponseWriter, tmpl string, p *Page) {
	err := templates.ExecuteTemplate(w, tmpl+".html", p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// summaryHandler
// ===
func summaryHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	orderBy := r.URL.Query().Get("order_by")
	if orderBy == "" {
		orderBy = "start_time"
	} else {
		if !slices.Contains(FIELDS, orderBy) {
			http.Error(w, "Invalid order_by parameter. Must be one of: "+strings.Join(FIELDS, ", "), http.StatusBadRequest)
			return
		}
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
	var args []interface{}
	var droplist []string

	switch qt {
	case UNALLOCATED:
		typ := r.URL.Query().Get(TYPE_PARAM)
		whereClause = "region is null and type = ?"
		args = []interface{}{typ}
		qp = TYPE_PARAM + "=" + typ
		desc = typ + "s not allocated to a region"
		droplist = append(droplist, "Type", "Region", "Level")
	case REGION_AND_TYPE:
		region := r.URL.Query().Get(REGION_PARAM)
		typ := r.URL.Query().Get(TYPE_PARAM)
		whereClause = "region = ? and type = ?"
		args = []interface{}{region, typ}
		qp = REGION_PARAM + "=" + region + "&" + TYPE_PARAM + "=" + typ
		desc = typ + "s in " + region
		droplist = append(droplist, "Type", "Region")
	case REGION:
		region := r.URL.Query().Get(REGION_PARAM)
		whereClause = "region = ?"
		args = []interface{}{region}
		qp = REGION_PARAM + "=" + region
		desc = "Tracks in " + region
		droplist = append(droplist, "Region", "Level")
	case TYPE:
		typ := r.URL.Query().Get(TYPE_PARAM)
		whereClause = "type = ?"
		args = []interface{}{typ}
		qp = TYPE_PARAM + "=" + typ
		desc = typ + "s"
		droplist = append(droplist, "Type")
	case YEAR:
		year := r.URL.Query().Get(YEAR_PARAM)
		typ := r.URL.Query().Get(TYPE_PARAM)
		whereClause = "year(start_time) = ? and type = ?"
		args = []interface{}{year, typ}
		qp = YEAR_PARAM + "=" + year + "&" + TYPE_PARAM + "=" + typ
		desc = typ + "s in " + year
		droplist = append(droplist, "Type")
	case TRACK_ID_IN:
		idsStr := r.URL.Query().Get(TRACK_ID_IN_PARAM)
		ids := strings.Split(idsStr, ",")
		qmarks := strings.Repeat("?,", len(ids))
		qmarks = qmarks[:len(qmarks)-1] // Remove trailing comma
		whereClause = "track_id in (" + qmarks + ")"
		args = make([]interface{}, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		qp = TRACK_ID_IN_PARAM + "=" + idsStr
		desc = "Tracks with IDs matching " + idsStr
	case DATE_RANGE:
		start_date := r.URL.Query().Get(DATE_RANGE_START_PARAM)
		end_date := r.URL.Query().Get(DATE_RANGE_END_PARAM)
		whereClause = "start_time >= ? AND start_time <= ?"
		args = []interface{}{start_date, end_date}
		qp = DATE_RANGE_START_PARAM + "=" + start_date + "&" + DATE_RANGE_END_PARAM + "=" + end_date
		desc = "Tracks from " + start_date + " to " + end_date
	case DISTANCE_RANGE:
		shortest_distance := r.URL.Query().Get(DISTANCE_RANGE_MIN_PARAM)
		longest_distance := r.URL.Query().Get(DISTANCE_RANGE_MAX_PARAM)
		typ := r.URL.Query().Get(TYPE_PARAM)

		if longest_distance == "" {
			whereClause = "length_miles >= (? + 0.0)"
			args = []interface{}{shortest_distance}
			qp = DISTANCE_RANGE_MIN_PARAM + "=" + shortest_distance
			desc = " longer than " + shortest_distance + " miles"
		} else if shortest_distance == "" {
			whereClause = "length_miles <= (? + 0.0)"
			args = []interface{}{longest_distance}
			qp = DISTANCE_RANGE_MAX_PARAM + "=" + longest_distance
			desc = " shorter than " + longest_distance + " miles"
		} else {
			whereClause = "length_miles >= (? + 0.0) AND length_miles <= (? + 0.0)"
			args = []interface{}{shortest_distance, longest_distance}
			qp = DISTANCE_RANGE_MIN_PARAM + "=" + shortest_distance + "&" + DISTANCE_RANGE_MAX_PARAM + "=" + longest_distance
			desc = " between " + shortest_distance + " and " + longest_distance + " miles"
		}

		if typ != "" {
			whereClause += " AND type = ?"
			args = append(args, typ)
			qp += "&" + TYPE_PARAM + "=" + typ
			droplist = append(droplist, "Type")
			desc = typ + "s" + desc
		} else {
			desc = "Tracks" + desc
		}

	default:
	}
	allTracks := readTracks(db, orderBy, order, whereClause, args)

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
	end := min(offset+pageSize, totalTracks)

	// Get tracks for current page
	var pageTracks []Track
	if offset < totalTracks {
		pageTracks = allTracks[offset:end]
		for i := range pageTracks {
			pageTracks[i].SeqNum = offset + i + 1
		}
	}

	page := &Page{
		Title:           desc,
		Tracks:          pageTracks,
		CurrentPage:     currentPage,
		PageSize:        pageSize,
		TotalTracks:     totalTracks,
		TotalPages:      totalPages,
		OrderBy:         orderBy,
		Order:           order,
		QueryType:       qt,
		QueryParameters: qp,
		DropList:        droplist,
	}

	renderTemplate(w, "summary", page)
}

// parsePositiveInt
// ===
func parsePositiveInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}

// readTracks
// ===
func readTracks(db *sql.DB, orderBy string, order string, whereClause string, args []interface{}) []Track {

	fieldList := strings.Join(FIELDS, ", ")
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

	rows, err := stmt.Query(args...)
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

// openDatabase
// ===
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

// readConfigFile
// ===
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

// main
// ===
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

	fs := http.FileServer(http.Dir("./html"))
	mux := http.NewServeMux()
	mux.Handle("/html/", http.StripPrefix("/html/", fs))

	mux.HandleFunc("/summary/", func(w http.ResponseWriter, r *http.Request) {
		summaryHandler(w, r, db)
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}
