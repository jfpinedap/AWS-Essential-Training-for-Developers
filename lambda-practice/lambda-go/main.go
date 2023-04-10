package main

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-lambda-go/lambda"
)

type MyEvent struct {
	Name string `json:"name"`
}

func handler(ctx context.Context, event MyEvent) (string, error) {
	return fmt.Sprintf("Hello, %s!", event.Name), nil
}

func main() {
	log.Printf("This is a log test")
	lambda.Start(handler)
}
