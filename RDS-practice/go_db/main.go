package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"

	_ "github.com/go-sql-driver/mysql"
)

type Image struct {
	Name       string    `db:"name"`
	Size       int64     `db:"size"`
	Extension  string    `db:"extension"`
	LastUpdate time.Time `db:"last_update"`
}

var (
	db          *sql.DB
	dbUser      string
	dbPass      string
	dbHost      string
	dbPort      string
	dbName      string
	dbTableName string
	timeLayout  string = "2006-01-02 15:04:05"
)

func main() {
	readEnv()

	// Connect to the database
	var err error
	DNS := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbHost, dbPort, dbName)
	db, err = sql.Open("mysql", DNS)
	if err != nil {
		panic(err.Error())
	}
	defer db.Close()

	createTable()
	image := Image{
		Name:       "test1",
		Size:       10,
		Extension:  "png",
		LastUpdate: time.Now(),
	}
	idAdded, _ := insertImage(image)
	log.Printf("Id: %d Image: %s", idAdded, image)

	if (idAdded % 13) == 0 {
		numRowsDeleted, _ := deleteImageByName("test1")
		log.Printf("Number of rows deleted: %d", numRowsDeleted)
	}

	images, _ := getAllImages()
	logImages(images)

	log.Println("Buuu")
	image, _ = getRandomImages()
	logImages([]Image{image})
}

func createTable() error {
	// Create the table if it doesn't exist
	result, err := db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id int NOT NULL AUTO_INCREMENT,
			name VARCHAR(255) NOT NULL,
			size INT NOT NULL,
			extension VARCHAR(255) NOT NULL,
			last_update DATETIME NOT NULL,
			PRIMARY KEY (id)
		)`, dbTableName))
	if err != nil {
		panic(err.Error())
	}
	log.Printf("Result: %s", result)
	log.Printf("Table '%s' created or already exists.\n", dbTableName)
	return err
}

func insertImage(image Image) (int64, error) {
	stmt, err := db.Prepare(fmt.Sprintf(
		"INSERT INTO %s(name, size, extension, last_update) VALUES( ?, ?, ?, ? )",
		dbTableName,
	))
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close() // Prepared statements take up server resources and should be closed after use.

	result, err := stmt.Exec(image.Name, image.Size, image.Extension, image.LastUpdate)
	if err != nil {
		log.Fatal(err)
	}
	// Get the new album's generated ID for the client.
	id, err := result.LastInsertId()
	if err != nil {
		log.Fatalf("AddAlbum: %v", err)
		return 0, err
	}
	// Return the new album's ID.
	return id, nil
}

func getAllImages() ([]Image, error) {
	stmt, err := db.Prepare(fmt.Sprintf(
		"SELECT name, size, extension, last_update FROM %s",
		dbTableName,
	))
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close() // Prepared statements take up server resources and should be closed after use.

	rows, err := stmt.Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []Image
	for rows.Next() {
		var image Image
		var lastUpdateStr string
		if err := rows.Scan(
			&image.Name,
			&image.Size,
			&image.Extension,
			&lastUpdateStr,
		); err != nil {
			log.Fatal(err)
		}
		image.LastUpdate, _ = time.Parse(timeLayout, lastUpdateStr)
		images = append(images, image)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}
	return images, nil
}

func getRandomImages() (Image, error) {
	stmt, err := db.Prepare(fmt.Sprintf(
		"SELECT name, size, extension, last_update FROM %s ORDER BY RAND() LIMIT 1",
		dbTableName,
	))
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close() // Prepared statements take up server resources and should be closed after use.

	var image Image
	var lastUpdateStr string

	err = stmt.QueryRow().Scan(
		&image.Name,
		&image.Size,
		&image.Extension,
		&lastUpdateStr,
	)
	image.LastUpdate, _ = time.Parse(timeLayout, lastUpdateStr)
	if err != nil {
		return image, err
	}
	return image, nil
}

func deleteImageByName(name string) (int64, error) {
	stmt, err := db.Prepare(fmt.Sprintf(
		"DELETE FROM %s WHERE name=?",
		dbTableName,
	))
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close() // Prepared statements take up server resources and should be closed after use.

	result, err := stmt.Exec(name)
	if err != nil {
		log.Fatal(err)
	}
	// Get the new album's generated ID for the client.
	numRowsDeleted, err := result.RowsAffected()
	if err != nil {
		log.Fatalf("AddAlbum: %v", err)
		return 0, err
	}
	// Return the new album's ID.
	return numRowsDeleted, nil
}

func logImages(images []Image) {
	for _, image := range images {
		log.Printf(
			"Name: %d, Size: %d, Extension: %s, Last Update: %s",
			image.Name,
			image.Size,
			image.Extension,
			image.LastUpdate,
		)
	}
}

func readEnv() {
	// read .env
	err := godotenv.Load(".env")
	if err != nil {
		panic("Error loading .env file")
	}
	dbUser = os.Getenv("DB_USER")
	dbPass = os.Getenv("DB_PASS")
	dbHost = os.Getenv("DB_HOST")
	dbPort = os.Getenv("DB_PORT")
	dbName = os.Getenv("DB_NAME")
	dbTableName = os.Getenv("DB_TABLENAME")
}
