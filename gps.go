package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"github.com/go-sql-driver/mysql"
)

// ====================================================================================================================
var db *sql.DB

func main() {
	// Open our jsonFile
	jsonFile, err := os.Open("C:/Users/simonf/Documents/GPS/server/cgi-bin/gps_config.json")
	// if we os.Open returns an error then handle it
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
		User:   jdb["user"].(string),
		Passwd: jdb["password"].(string),
		Net:    "tcp",
		Addr:   jdb["host"].(string)+":3306",
		DBName: jdb["schema"].(string),
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

	var id int
	var name string
	var description string
	var categoryid string
	for rows.Next() {
		err = rows.Scan(&id, &name, &description, &categoryid)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("ID: %d, Name: %s, Description: %s, Category ID: %s\n", id, name, description, categoryid)
	}
}

// ====================================================================================================================
