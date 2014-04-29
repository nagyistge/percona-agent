package main

import (
	"fmt"
	golog "log"
)

const (
	//CLOUD_API_HOSTNAME = "https://cloud-api.percona.com"
	CLOUD_API_HOSTNAME = "http://localhost:8000"
)

func init() {
	golog.SetFlags(golog.Ldate | golog.Ltime | golog.Lmicroseconds | golog.Lshortfile)
}

func main() {
	installer := NewInstaller()

	var err error
	// Server
	err = installer.GetHostname()
	if err != nil {
		golog.Fatalf("Unable to get hostname: %s", err)
	}

	// Api key
	for {
		// Ask user for api key
		err = installer.GetApiKey()
		if err != nil {
			golog.Fatalf("Unable to get API key: %s", err)
		}

		// Check if api key is correct
		err = installer.VerifyApiKey()
		if err == ErrApiUnauthorized {
			fmt.Printf("Unauthorized, check if API key is correct and try again")
			fmt.Println()
			continue
		} else if err != nil {
			fmt.Printf("Unable to verify API key: %s. Contact Percona Support if this error persists.", err)
			fmt.Println()
			continue
		}

		break // success
	}

	// Api entry Links
	err = installer.GetApiLinks()
	if err != nil {
		golog.Fatalf("Unable to get API entry links: %s", err)
	}
	err = installer.VerifyApiLinks()
	if err != nil {
		golog.Fatalf("Bad API entry links: %s", err)
	}

	// Mysql connection details
	for {
		// Ask user for api key
		err = installer.GetMysqlDsn()
		if err != nil {
			golog.Fatalf("An exception occured: %s", err)
		}

		// Check if api key is correct
		err = installer.VerifyMysqlDsn()
		if err != nil {
			fmt.Printf("Unable to verify mysql dsn: %s", err)
			fmt.Println()
			continue
		}

		break // success
	}

	// Create agent
	err = installer.CreateAgent()
	if err != nil {
		golog.Fatalf("Unable to create agent: %s", err)
	}

	installer.Close()
}

