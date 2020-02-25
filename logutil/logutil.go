package logutil

import (
	"log"
	"os"
)

var IsDev = false

func init() {
	IsDev = os.Getenv("INDIHUB_DEV") != ""
}

func LogError(format string, args ...interface{}) {
	if !IsDev {
		return
	}

	log.Printf(format, args...)
}
