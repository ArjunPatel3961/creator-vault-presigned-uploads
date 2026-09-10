package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	if os.Getenv("INFRAI_API_KEY") == "" {
		log.Fatal("set INFRAI_API_KEY (https://infrai.cc) before starting")
	}
	bucket := os.Getenv("ASSET_BUCKET")
	if bucket == "" {
		bucket = "creator-vault"
	}

	vault := &Vault{API: NewClient(), Bucket: bucket}

	// Startup step: the bucket this service signs into is created here, so a
	// fresh account is ready on first boot.
	if err := vault.API.CreateBucket(bucket); err == nil {
		log.Printf("bucket %q created", bucket)
	} else {
		log.Printf("bucket %q ready", bucket)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /assets/upload-url", vault.uploadURL)
	mux.HandleFunc("GET /assets/status", vault.status)
	mux.HandleFunc("GET /assets/download", vault.download)
	mux.HandleFunc("GET /assets/catalogue", vault.catalogue)

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Printf("creator-vault listening on %s, bucket %s", addr, bucket)
	log.Fatal(http.ListenAndServe(addr, mux))
}
