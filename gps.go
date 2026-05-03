// Package main implements GPS, a web application for managing GPS tracking data.
// It provides HTTP endpoints for querying and filtering GPS tracks from a MySQL database,
// with support for filtering by region, type, date range, distance, and track ID.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strconv"
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

type RegionEntry struct {
	Region        string  `db:"region"`
	Tracks        int     `db:"tracks"`
	TotalDistance float32 `db:"total_distance"`
	Shortest      float32 `db:"shortest"`
	Average       float32 `db:"average"`
	Longest       float32 `db:"longest"`
	Type          string  `db:"type"`
	SeqNum        int
}

type TypeEntry struct {
	Type          string  `db:"type"`
	Tracks        int     `db:"tracks"`
	TotalDistance float32 `db:"total_distance"`
	Shortest      float32 `db:"shortest_distance"`
	Average       float32 `db:"average_distance"`
	Longest       float32 `db:"longest_distance"`
	SeqNum        int
}

type YearEntry struct {
	Year          int     `db:"year"`
	Tracks        int     `db:"tracks"`
	TotalDistance float32 `db:"total_distance"`
	Shortest      float32 `db:"shortest_distance"`
	Average       float32 `db:"average_distance"`
	Longest       float32 `db:"longest_distance"`
	Type          string  `db:"type"`
	SeqNum        int
}

type RegionsEntry struct {
	Id          int     `db:"id"`
	Description string  `db:"description"`
	West        float32 `db:"west"`
	East        float32 `db:"east"`
	North       float32 `db:"north"`
	South       float32 `db:"south"`
	Level       int     `db:"level"`
	SeqNum      int
}

type SummaryPage struct {
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

// google geocode xml response
type GeocodeResponse struct {
	Status  string   `xml:"status"`
	Results []Result `xml:"result"`
}

type Result struct {
	Type     string   `xml:"type"`
	Geometry Geometry `xml:"geometry"`
	Address  string   `xml:"formatted_address"`
}

type Geometry struct {
	Location Location `xml:"location"`
}

type Location struct {
	Lat string `xml:"lat"`
	Lng string `xml:"lng"`
}

// Globals
// ===
var templates *template.Template

// pageLink
// ===
func pageLink(p *SummaryPage, order_by, order string) template.URL {
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
func renderTemplate(w http.ResponseWriter, tmpl string, p any) {
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

	page := &SummaryPage{
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

func latestwalkHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	renderTemplate(w, "latestwalk", nil)
}
func byregionHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	renderTemplate(w, "byregion", readByRegion(db))
}
func bytypeHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	renderTemplate(w, "bytype", readByType(db))
}
func byyearHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	renderTemplate(w, "byyear", readByYear(db))
}
func regionsHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	renderTemplate(w, "regions", readRegions(db))
}
func tracksearchHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	renderTemplate(w, "tracksearch", nil)
}
func uploadsHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	renderTemplate(w, "uploads", nil)
}
func databasestatsHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	renderTemplate(w, "databasestats", nil)
}

// Tracksearch handlers
func coordsHandler(w http.ResponseWriter, r *http.Request, db *sql.DB, apikey1 string) {
	var geocodeResp GeocodeResponse
	location := r.URL.Query().Get("location")
	toleranceStr := r.URL.Query().Get("tolerance")
	tolerance, err := strconv.ParseFloat(toleranceStr, 32)
	if err != nil {
		http.Error(w, "Invalid tolerance value", http.StatusBadRequest)
		return
	}

	lat := r.URL.Query().Get("lat")
	if lat != "" {
		// if lat is provided, we assume lng is also provided and we just return the location as the address, without calling the geocoding API
		// mimic a response from the google api
		geocodeResp.Status = "OK"
		geocodeResp.Results = append(geocodeResp.Results, Result{
			Type: "typexxx",
			Geometry: Geometry{
				Location: Location{
					Lat: lat,
					Lng: r.URL.Query().Get("long"),
				},
			},
			Address: "addressxxx",
		})
	} else {
		// query the google api given the name of the location, and return the lat and lng
		url := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/xml?key=%s&address=%s", apikey1, location)
		// Simple GET request
		resp, err := http.Get(url)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()

		// Read the response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		//fmt.Printf("response = %s", body)

		// Parse the XML response
		err = xml.Unmarshal(body, &geocodeResp)
		if err != nil {
			http.Error(w, "Error parsing XML: ", http.StatusInternalServerError)
			return
		}
	}

	if geocodeResp.Status == "OK" {
		var matchingTrackIds []int
		for _, result := range geocodeResp.Results {
			/*
				fmt.Println("Geocode Type:", result.Type)
				fmt.Println("Geocode Lat:", result.Geometry.Location.Lat)
				fmt.Println("Geocode Lng:", result.Geometry.Location.Lng)
				fmt.Println("Geocode Address:", result.Address)
			*/
			fLat, err := strconv.ParseFloat(result.Geometry.Location.Lat, 64)
			if err != nil {
				http.Error(w, "Error parsing latitude: "+err.Error(), http.StatusBadRequest)
				return
			}
			fLng, err := strconv.ParseFloat(result.Geometry.Location.Lng, 64)
			if err != nil {
				http.Error(w, "Error parsing longitude: "+err.Error(), http.StatusBadRequest)
				return
			}
			matchingTrackIds = append(matchingTrackIds, getMatchingTracks(db, fLat, fLng, tolerance)...)
		}
		//fmt.Println("Matching Track IDs:", matchingTrackIds)
		switch len(matchingTrackIds) {
		case 0:
			http.Error(w, "No matching tracks found", http.StatusNotFound)
		default:
			idsStr := make([]string, len(matchingTrackIds))
			for i, id := range matchingTrackIds {
				idsStr[i] = strconv.Itoa(id)
			}
			http.Redirect(w, r, "/summary?qt=trackidin&ids="+strings.Join(idsStr, ","), http.StatusSeeOther)
		}
	} else {
		http.Error(w, "Geocoding API error: "+geocodeResp.Status, http.StatusInternalServerError)
	}
}

func getMatchingTracks(db *sql.DB, lat float64, lng float64, tolerance float64) []int {
	west := lng - tolerance
	east := lng + tolerance
	north := lat + tolerance
	south := lat - tolerance

	query := "SELECT track_id FROM points, tracks WHERE longitude >= ? AND longitude <= ? AND latitude <= ? AND latitude >= ? AND points.track_id=tracks.id GROUP BY track_id, source ORDER BY track_id;"

	args := []interface{}{west, east, north, south}
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

	trackIds := []int{}
	for rows.Next() {
		var trackId int
		err = rows.Scan(&trackId)
		if err != nil {
			log.Fatal(err)
		}
		trackIds = append(trackIds, trackId)
	}
	return trackIds
}

var MapCoords = make(map[string][2]float64)

func grHandler(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	gr := `400 1200 HP
300 1100 HT HU
100 1000 HW HX HY HZ
0 900 NA NB NC ND
0 800 NF NG NH NJ NK
0 700 NL NM NN NO
100 600 NR NS NT NU
100 500 NW NX NY NZ OV
200 400 SC SD SE TA
200 300 SH SJ SK TF TG
100 200 SM SN SO SP TL TM
100 100 SR SS ST SU TQ TR
0 0 SV SW SZ SY SZ TV`

	lines := strings.Split(gr, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		e, err := strconv.Atoi(fields[0])
		if err != nil {
			log.Fatal(err)
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			log.Fatal(err)
		}
		for i, code := range fields[2:] {
			// Process each code
			MapCoords[code] = [2]float64{(float64(e) + float64(i)*100.0) * 1000.0, float64(n) * 1000.0}
		}
	}

	gridref := r.URL.Query().Get("gridref")
	toleranceStr := r.URL.Query().Get("tolerance")
	tolerance, err := strconv.ParseFloat(toleranceStr, 64)
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Printf("gridref = %s, tolerance = %f", gridref, tolerance)
	lat, lng := gr2ll(gridref)

	http.Redirect(w, r, "/coords?lat="+strconv.FormatFloat(lat, 'f', -1, 64)+"&long="+strconv.FormatFloat(lng, 'f', -1, 64)+"&tolerance="+strconv.FormatFloat(tolerance, 'f', -1, 64), http.StatusSeeOther)
}

func gr2ll(gr string) (float64, float64) {
	sq := gr[0:2]

	var e, n float64
	switch len(gr) {
	case 8:
		eStr := gr[2:5]
		nStr := gr[5:]
		e, err := strconv.ParseFloat(eStr, 64)
		if err != nil {
			log.Fatal(err)
		}
		n, err := strconv.ParseFloat(nStr, 64)
		if err != nil {
			log.Fatal(err)
		}
		e *= 100.0
		n *= 100.0
	case 10:
		eStr := gr[2:6]
		nStr := gr[6:]
		e, err := strconv.ParseFloat(eStr, 64)
		if err != nil {
			log.Fatal(err)
		}
		n, err := strconv.ParseFloat(nStr, 64)
		if err != nil {
			log.Fatal(err)
		}
		e *= 10.0
		n *= 10.0
	default:
		log.Fatal("Invalid grid reference length")
	}

	return getll(MapCoords[sq][0]+e, MapCoords[sq][1]+n)
}

func getll(e float64, n float64) (float64, float64) {
	url := fmt.Sprintf("https://webapps.bgs.ac.uk/data/webservices/CoordConvert_LL_BNG.cfc?method=BNGtoLatLng&easting=%f&northing=%f", e, n)
	resp, err := http.Post(url, "application/x-www-form-urlencoded", bytes.NewBuffer([]byte("query=libwwww-perl&mode=dist")))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	var responseMap map[string]interface{}
	err = json.Unmarshal(body, &responseMap)
	if err != nil {
		log.Fatal(err)
	}
	fLat, ok := responseMap["LATITUDE"].(float64)
	if !ok {
		log.Fatal("invalid LATITUDE value")
	}
	fLng, ok := responseMap["LONGITUDE"].(float64)
	if !ok {
		log.Fatal("invalid LONGITUDE value")
	}

	return fLat, fLng
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

// readByRegion
// ===
func readByRegion(db *sql.DB) []RegionEntry {
	query := "SELECT region, tracks, total_distance, shortest, average, longest, type FROM ByRegion ORDER BY region ASC"

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

	regionEntries := []RegionEntry{}
	i := 0
	for rows.Next() {
		regionEntry := RegionEntry{}
		err = rows.Scan(
			&regionEntry.Region,
			&regionEntry.Tracks,
			&regionEntry.TotalDistance,
			&regionEntry.Shortest,
			&regionEntry.Average,
			&regionEntry.Longest,
			&regionEntry.Type,
		)
		regionEntry.SeqNum = i
		i++
		if err != nil {
			log.Fatal(err)
		}
		regionEntries = append(regionEntries, regionEntry)
	}
	return regionEntries
}

// readByType
// ===
func readByType(db *sql.DB) []TypeEntry {
	query := "SELECT type, tracks, total_distance, shortest, average, longest FROM ByType ORDER BY type ASC"

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

	typeEntries := []TypeEntry{}
	i := 0
	for rows.Next() {
		typeEntry := TypeEntry{}
		err = rows.Scan(
			&typeEntry.Type,
			&typeEntry.Tracks,
			&typeEntry.TotalDistance,
			&typeEntry.Shortest,
			&typeEntry.Average,
			&typeEntry.Longest,
		)
		typeEntry.SeqNum = i
		i++
		if err != nil {
			log.Fatal(err)
		}
		typeEntries = append(typeEntries, typeEntry)
	}
	return typeEntries
}

// readByYear
// ===
func readByYear(db *sql.DB) []YearEntry {
	query := "SELECT year, tracks, total_distance, shortest, average, longest, type FROM ByYear ORDER BY type DESC, year ASC"

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

	yearEntries := []YearEntry{}
	i := 0
	for rows.Next() {
		yearEntry := YearEntry{}
		err = rows.Scan(
			&yearEntry.Year,
			&yearEntry.Tracks,
			&yearEntry.TotalDistance,
			&yearEntry.Shortest,
			&yearEntry.Average,
			&yearEntry.Longest,
			&yearEntry.Type,
		)
		yearEntry.SeqNum = i
		i++
		if err != nil {
			log.Fatal(err)
		}
		yearEntries = append(yearEntries, yearEntry)
	}
	return yearEntries
}

// readRegions
// ===
func readRegions(db *sql.DB) []RegionsEntry {
	query := "SELECT id, description, west, east, north, south, level FROM regions ORDER BY description ASC"

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

	regionsEntries := []RegionsEntry{}
	i := 0
	for rows.Next() {
		regionsEntry := RegionsEntry{}
		err = rows.Scan(
			&regionsEntry.Id,
			&regionsEntry.Description,
			&regionsEntry.West,
			&regionsEntry.East,
			&regionsEntry.North,
			&regionsEntry.South,
			&regionsEntry.Level,
		)
		regionsEntry.SeqNum = i
		i++
		if err != nil {
			log.Fatal(err)
		}
		regionsEntries = append(regionsEntries, regionsEntry)
	}
	return regionsEntries
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
	apikey1 := configMap["apikey1"].(string)
	fmt.Println(databaseInfo["dbtype"])
	db := openDatabase(databaseInfo)
	defer db.Close()

	pingErr := db.Ping()
	if pingErr != nil {
		log.Fatal(pingErr)
	}
	fmt.Println("Connected!")

	templates = GetTemplates()

	fs := http.FileServer(http.Dir("./html"))
	mux := http.NewServeMux()

	// hand off for static files in the html directory
	mux.Handle("/html/", http.StripPrefix("/html/", fs))

	mux.HandleFunc("/latestwalk/", func(w http.ResponseWriter, r *http.Request) {
		latestwalkHandler(w, r, db)
	})
	mux.HandleFunc("/byregion/", func(w http.ResponseWriter, r *http.Request) {
		byregionHandler(w, r, db)
	})
	mux.HandleFunc("/bytype/", func(w http.ResponseWriter, r *http.Request) {
		bytypeHandler(w, r, db)
	})
	mux.HandleFunc("/byyear/", func(w http.ResponseWriter, r *http.Request) {
		byyearHandler(w, r, db)
	})
	mux.HandleFunc("/regions/", func(w http.ResponseWriter, r *http.Request) {
		regionsHandler(w, r, db)
	})
	mux.HandleFunc("/tracksearch/", func(w http.ResponseWriter, r *http.Request) {
		tracksearchHandler(w, r, db)
	})
	mux.HandleFunc("/uploads/", func(w http.ResponseWriter, r *http.Request) {
		uploadsHandler(w, r, db)
	})
	mux.HandleFunc("/databasestats/", func(w http.ResponseWriter, r *http.Request) {
		databasestatsHandler(w, r, db)
	})
	mux.HandleFunc("/summary/", func(w http.ResponseWriter, r *http.Request) {
		summaryHandler(w, r, db)
	})
	mux.HandleFunc("/gr/", func(w http.ResponseWriter, r *http.Request) {
		grHandler(w, r, db)
	})
	mux.HandleFunc("/coords/", func(w http.ResponseWriter, r *http.Request) {
		coordsHandler(w, r, db, apikey1)
	})
	log.Fatal(http.ListenAndServe(":8080", mux))
}
