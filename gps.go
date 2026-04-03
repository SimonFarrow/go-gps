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
	Name        string `db:"name"`
	Description string `db:"description"`
	CategoryID  string `db:"category_id"`
}

type Page struct {
	Title  string
	Tracks []Track
}

// ====================================================================================================================
// Globals
var templates = template.Must(template.ParseFiles("html/view.html"))
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
func makeHandler(fn func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := validPath.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		fn(w, r, m[2])
	}
}

// ====================================================================================================================
func viewHandler(w http.ResponseWriter, r *http.Request, title string) {
	//p, err := loadPage(title)
	//if err != nil {
	//	http.Redirect(w, r, "/edit/"+title, http.StatusFound)
	//	return
	//}
	p := Page{"GPS", nil}
	renderTemplate(w, "view", &p)
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
		err = rows.Scan(&track.ID, &track.Name, &track.Description, &track.CategoryID)
		if err != nil {
			log.Fatal(err)
		}
		tracks = append(tracks, track)
		fmt.Printf("ID: %d, Name: %s, Description: %s, Category ID: %s\n", track.ID, track.Name, track.Description, track.CategoryID)
	}

	http.HandleFunc("/view/", makeHandler(viewHandler))
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// ====================================================================================================================
