package main

import (
	"context"
	"log"
	"net/http"
	"strings"
)

func requestHandler(ctx context.Context) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if ctx.Err() != nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			log.Printf("Error [%d]: %s\n", http.StatusServiceUnavailable, "Service unavailable; terminating")
			return
		}

		if err := checkLimits(r); err != nil {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			log.Printf("Error [%d]: %s\n", http.StatusTooManyRequests, err)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			log.Printf("Error [%d]: %s\n", http.StatusMethodNotAllowed, "Invalid request method")
			return
		}

		installationToken, err := getInstallationToken()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			log.Printf("Error [%d]: %s\n", http.StatusInternalServerError, err)
			return
		}

		// Parse the request URL: /owner/repo/path/to/file
		parts := strings.SplitN(r.URL.Path, "/", 4)
		if len(parts) < 4 {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			log.Printf("Error [%d]: %s\n", http.StatusBadRequest, "Invalid request path")
			return
		}
		owner, repo, filePath := parts[1], parts[2], parts[3]

		log.Printf("incoming request: %s %s [owner: %s, repo: %s, path: %s]\n", r.Method, r.URL.Path, owner, repo, filePath)

		for _, elem := range strings.Split(filePath, "/") {
			if len(elem) > 0 && elem[0] == '.' {
				http.Error(w, "Permission Denied", http.StatusForbidden)
				log.Printf("Error [%d]: %s\n", http.StatusForbidden, "Permission denied")
				return
			}
		}

		content, contentType, err := GetFileContent(owner, repo, filePath, installationToken)
		if err != nil {
			http.Error(w, "File Not Found", http.StatusNotFound)
			log.Printf("Error [%d]: %s\n", http.StatusNotFound, err)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Write(content)
	}
}
