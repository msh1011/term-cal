package main

import (
	"testing"

	"github.com/joho/godotenv"
)

var (
	TestHostname = "http://localhost:8000"
)

func TestMain(t *testing.T) {
	godotenv.Load(".env")
	start(TestHostname)
}
