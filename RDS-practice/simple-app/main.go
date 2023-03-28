package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
)

var (
	db          *sql.DB
	s3Session   *session.Session
	s3Bucket    string
	awsRegion   string
	dbUser      string
	dbPass      string
	dbHost      string
	dbPort      string
	dbName      string
	dbTableName string
	timeLayout  string = "2006-01-02 15:04:05"
)

type Image struct {
	Name       string    `db:"name"`
	LastUpdate time.Time `db:"last_update"`
	Size       int64     `db:"size"`
	Extension  string    `db:"extension"`
}

func main() {
	readEnv()

	var err error
	DNS := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbHost, dbPort, dbName)
	db, err = sql.Open("mysql", DNS)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createTable()

	s3Session, err = session.NewSession(&aws.Config{
		Region: aws.String(awsRegion),
	})
	if err != nil {
		log.Fatal(err)
	}

	router := mux.NewRouter()
	router.HandleFunc("/image", getImage).Methods("GET")
	router.HandleFunc("/image", uploadImage).Methods("POST")
	router.HandleFunc("/image", deleteImage).Methods("DELETE")
	router.HandleFunc("/image/metadata", getAllMetadata).Methods("GET")
	router.HandleFunc("/image/random/metadata", getRandomMetadata).Methods("GET")

	log.Fatal(http.ListenAndServe(":8080", router))
}

func getImage(w http.ResponseWriter, r *http.Request) {
	// Get the value of the 'name' query parameter
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Please provide an image name through query parameters: http://domain/image?name=imageName.png", http.StatusBadRequest)
		return
	}

	// Create an S3 client and get the Image
	file, err := s3.New(s3Session).GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s3Bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		log.Println(err)
		http.Error(w, "Error downloading image", http.StatusInternalServerError)
		return
	}

	// Get the filename from the S3 object's metadata
	var filename string
	if len(file.Metadata) > 0 {
		filename = aws.StringValue(file.Metadata["filename"])
	} else {
		filename = "image.png"
	}

	// Set the headers to indicate that the file is downloadable
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Type", aws.StringValue(file.ContentType))
	w.Header().Set("Content-Length", strconv.FormatInt(aws.Int64Value(file.ContentLength), 10))

	log.Printf("File '%s' downloaded...", filename)
	if _, err := io.Copy(w, file.Body); err != nil {
		log.Println(err)
	}
}

func uploadImage(w http.ResponseWriter, r *http.Request) {
	imageFile, handler, err := r.FormFile("image")
	if err != nil {
		log.Println(err)
		http.Error(w, "Error uploading image", http.StatusBadRequest)
		return
	}
	defer imageFile.Close()

	uploader := s3manager.NewUploader(s3Session)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(s3Bucket),
		Key:    aws.String(handler.Filename),
		Body:   imageFile,
	})
	if err != nil {
		log.Println(err)
		http.Error(w, "Error uploading image to S3", http.StatusInternalServerError)
		return
	}

	image := Image{
		Name:       handler.Filename,
		Size:       handler.Size,
		Extension:  getFileExtension(handler.Filename),
		LastUpdate: time.Now(),
	}
	_, err = insertImage(image)
	if err != nil {
		log.Println(err)
		http.Error(w, "Error inserting metadata into RDS", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Image uploaded successfully")
}

func deleteImage(w http.ResponseWriter, r *http.Request) {
	// Get the value of the myId query parameter
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Please provide an image name through URL parameters: http://domain/image?name=imageName.png", http.StatusBadRequest)
		return
	}

	// Create an S3 client
	s3Svc := s3.New(s3Session)

	// Delete image from S3
	_, err := s3Svc.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(s3Bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		log.Println(err)
		http.Error(w, "Error deleting image from S3", http.StatusInternalServerError)
		return
	}

	// Delete metadata from RDS
	_, err = deleteImageByName(name)
	if err != nil {
		log.Println(err)
		http.Error(w, "Error deleting metadata from RDS", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Image '%s' deleted successfully", name)
}

func getAllMetadata(w http.ResponseWriter, r *http.Request) {
	images, _ := getAllImages()
	logImages(images)

	// Write the Image list as a JSON response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(images)
}

func getRandomMetadata(w http.ResponseWriter, r *http.Request) {
	image, _ := getRandomImage()
	logImages([]Image{image})

	// Write the Image as a JSON response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(image)
}

func getFileExtension(filename string) string {
	parts := strings.Split(filename, ".")
	return parts[len(parts)-1]
}

func createTable() error {
	// Create the table if it doesn't exist
	_, err := db.Exec(fmt.Sprintf(`
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

func getRandomImage() (Image, error) {
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
	s3Bucket = os.Getenv("S3_BUCKET")
	awsRegion = os.Getenv("AWS_REGION")
	dbUser = os.Getenv("DB_USER")
	dbPass = os.Getenv("DB_PASS")
	dbHost = os.Getenv("DB_HOST")
	dbPort = os.Getenv("DB_PORT")
	dbName = os.Getenv("DB_NAME")
	dbTableName = os.Getenv("DB_TABLENAME")
}
