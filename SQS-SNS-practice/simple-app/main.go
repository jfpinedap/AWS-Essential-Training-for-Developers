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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/ssm"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
)

var (
	db          *sql.DB
	awsSession  *session.Session
	s3Bucket    string
	awsRegion   string = "us-east-1"
	dbUser      string
	dbPass      string
	dbHost      string
	dbPort      string
	dbName      string
	dbTableName string
	topicARN    string
	queueURL    string
	timeLayout  string = "2006-01-02 15:04:05"
)

type Image struct {
	Name       string    `db:"name"`
	LastUpdate time.Time `db:"last_update"`
	Size       int64     `db:"size"`
	Extension  string    `db:"extension"`
}

func main() {
	// Initialize a new session using the default AWS configuration.
	var err error
	awsSession, err = session.NewSession(&aws.Config{
		Region: aws.String(awsRegion),
	})
	if err != nil {
		log.Fatal(err)
	}

	readEnv()

	DNS := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", dbUser, dbPass, dbHost, dbPort, dbName)
	db, err = sql.Open("mysql", DNS)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createTable()

	// Define routes and their handlers
	router := mux.NewRouter()
	router.HandleFunc("/image", getImage).Methods("GET")
	router.HandleFunc("/image", uploadImage).Methods("POST")
	router.HandleFunc("/image", deleteImage).Methods("DELETE")
	router.HandleFunc("/image/metadata", getAllMetadata).Methods("GET")
	router.HandleFunc("/image/random/metadata", getRandomMetadata).Methods("GET")
	router.HandleFunc("/notification/subscription", subscribeEmail).Methods("GET")
	router.HandleFunc("/notification/unsubscription", unsubscription).Methods("GET")

	// Start the background process for sending SQS messages to SNS topic
	go pollSQSAndSendToSNS(60)

	// Start the web server
	log.Fatal(http.ListenAndServe(":8080", router))
}

func pollSQSAndSendToSNS(pollIntervalSecs int) {
	sqsClient := sqs.New(awsSession)
	snsClient := sns.New(awsSession)

	// Set up polling interval
	pollInterval := time.Duration(pollIntervalSecs) * time.Second

	// Loop continuously, polling for messages and sending them to SNS
	for {
		// Poll SQS for up to 10 messages at a time
		resp, err := sqsClient.ReceiveMessage(&sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: aws.Int64(10),
			WaitTimeSeconds:     aws.Int64(20),
		})
		if err != nil {
			log.Printf("Error receiving messages from SQS: %v\n", err)
			time.Sleep(pollInterval)
			continue
		}

		// If there are no messages, wait and continue
		if len(resp.Messages) == 0 {
			time.Sleep(pollInterval)
			continue
		}

		// Send each message to the SNS topic
		for _, msg := range resp.Messages {
			_, err := snsClient.Publish(&sns.PublishInput{
				Message:  aws.String(*msg.Body),
				TopicArn: aws.String(topicARN),
			})
			if err != nil {
				log.Printf("Error publishing message to SNS: %v\n", err)
			} else {
				log.Printf("Published message to SNS: %s\n", *msg.Body)
			}

			// Delete the message from SQS
			_, err = sqsClient.DeleteMessage(&sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
			if err != nil {
				log.Printf("Error deleting message from SQS: %v\n", err)
			} else {
				log.Printf("Deleted message from SQS: %s\n", *msg.Body)
			}
		}

		// Wait for the polling interval
		time.Sleep(pollInterval)
	}
}

func subscribeEmail(w http.ResponseWriter, r *http.Request) {
	// Get the email address from the query parameters
	email := r.URL.Query().Get("email")
	if email == "" {
		http.Error(w, "Missing email parameter", http.StatusBadRequest)
		return
	}

	// Create an SNS client
	snsSvc := sns.New(awsSession)

	// Subscribe the email to the SNS topic
	subResp, err := snsSvc.Subscribe(&sns.SubscribeInput{
		Protocol: aws.String("email"),
		Endpoint: aws.String(email),
		TopicArn: aws.String(topicARN),
	})
	if err != nil {
		log.Printf("Error subscribing email to topic: %s \n %s", email, err)
		http.Error(w, "Error subscribing email to topic: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the subscription ARN as JSON
	resp := struct {
		SubscriptionARN string `json:"subscriptionARN"`
	}{
		SubscriptionARN: *subResp.SubscriptionArn,
	}
	json.NewEncoder(w).Encode(resp)
}

func unsubscription(w http.ResponseWriter, r *http.Request) {
	// Get the email address from the query parameters
	email := r.URL.Query().Get("email")
	if email == "" {
		http.Error(w, "Missing email parameter", http.StatusBadRequest)
		return
	}

	// Create an SNS client
	snsSvc := sns.New(awsSession)

	// List the subscriptions for the topic to find the subscription ARN for the email
	listResp, err := snsSvc.ListSubscriptionsByTopic(&sns.ListSubscriptionsByTopicInput{
		TopicArn: aws.String(topicARN),
	})
	if err != nil {
		http.Error(w, "Error listing subscriptions for topic: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var subARN string
	for _, sub := range listResp.Subscriptions {
		if *sub.Protocol == "email" && *sub.Endpoint == email {
			subARN = *sub.SubscriptionArn
			break
		}
	}
	if subARN == "" {
		http.Error(w, "Email is not subscribed to the topic", http.StatusBadRequest)
		return
	}

	// Unsubscribe the email from the SNS topic
	_, err = snsSvc.Unsubscribe(&sns.UnsubscribeInput{
		SubscriptionArn: aws.String(subARN),
	})
	if err != nil {
		http.Error(w, "Error unsubscribing email from topic: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return success message as JSON
	resp := struct {
		Message string `json:"message"`
	}{
		Message: "Email unsubscribed successfully",
	}
	json.NewEncoder(w).Encode(resp)
}

func getImage(w http.ResponseWriter, r *http.Request) {
	// Get the value of the 'name' query parameter
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Please provide an image name through query parameters: http://domain/image?name=imageName.png", http.StatusBadRequest)
		return
	}

	// Create an S3 client and get the Image
	file, err := s3.New(awsSession).GetObject(&s3.GetObjectInput{
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

	uploader := s3manager.NewUploader(awsSession)
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

	publishEventToSQS(image)
	fmt.Fprintf(w, "Image uploaded successfully")
}

func publishEventToSQS(image Image) {
	// Create a new SQS service client
	svc := sqs.New(awsSession)

	bodyMessage, _ := json.MarshalIndent(image, "", "  ")

	// Send a message to the SQS queue
	_, err := svc.SendMessage(&sqs.SendMessageInput{
		MessageBody: aws.String(string(bodyMessage)),
		QueueUrl:    aws.String(queueURL),
	})
	if err != nil {
		log.Fatalf("The image fail to load to SQS \n %s", bodyMessage)
	}
	log.Printf("Image loaded to SQS successfully \n %s", bodyMessage)
}

func deleteImage(w http.ResponseWriter, r *http.Request) {
	// Get the value of the myId query parameter
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Please provide an image name through URL parameters: http://domain/image?name=imageName.png", http.StatusBadRequest)
		return
	}

	// Create an S3 client
	s3Svc := s3.New(awsSession)

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
	s3Bucket = getParameter("s3Bucket")
	dbUser = getParameter("dbUser")
	dbPass = getParameter("dbPass")
	dbHost = getParameter("dbHost")
	dbPort = getParameter("dbPort")
	dbName = getParameter("dbName")
	dbTableName = getParameter("dbTableName")
	topicARN = getParameter("topicARN")
	queueURL = getParameter("queueURL")
}

func getParameter(paramName string) string {
	// Create a new Systems Manager client.
	svc := ssm.New(awsSession)
	// Retrieve the parameter value using its name.
	paramOutput, err := svc.GetParameter(&ssm.GetParameterInput{
		Name:           aws.String(paramName),
		WithDecryption: aws.Bool(false),
	})
	if err != nil || paramOutput == nil {
		fmt.Fprintf(os.Stderr, "failed to retrieve parameter value: %v", err)
		os.Exit(1)
	}
	value := *paramOutput.Parameter.Value
	log.Println(paramName, value)
	return value
}
