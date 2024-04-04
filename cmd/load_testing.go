package main

import (
	"flag"

	log "github.com/sirupsen/logrus"

	"rpc-test/load_testing"
)

type flags struct {
	providerUrl   string
	rateLimit     uint
	totalRequests uint
}

// Setup flags
func getFlags() (f flags) {
	flag.StringVar(&f.providerUrl, "providerUrl", "", "URL for test")
	flag.UintVar(&f.rateLimit, "rateLimit", 500, "provider rate limit")
	flag.UintVar(&f.totalRequests, "totalRequests", 100_000, "total test requests")
	flag.Parse()

	return
}

func main() {
	f := getFlags()
	err := setupLogger()
	if err != nil {
		log.Fatalf("Log setup: %s", err)
	}

	log.Infof("Start testing %s, rate limit: %d req/sec, total requests: %d", f.providerUrl, f.rateLimit, f.totalRequests)
	if f.providerUrl == "" {
		log.Error("Empty providerUrl")
		return
	}

	load_testing.StartTest(f.providerUrl, f.rateLimit, f.totalRequests)
}

func setupLogger() error {
	logLevel, err := log.ParseLevel("info")
	if err != nil {
		return err
	}

	// log format
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		DisableColors:   true,
	})

	log.SetLevel(logLevel)
	return nil
}
