package main

import (
	"bytes"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/golang-jwt/jwt/v4"
)

var (
	installationToken       string
	installationTokenExpiry time.Time
	tokenMutex              sync.Mutex
)

// getInstallationToken returns a valid installation token, renewing it if necessary.
func getInstallationToken() (string, error) {
	tokenMutex.Lock()
	defer tokenMutex.Unlock()

	if time.Now().Before(installationTokenExpiry) {
		log.Printf("using installation token from cache; expires at %s\n", installationTokenExpiry.Add(3*time.Minute))
		return installationToken, nil
	}

	log.Printf("acquiring new installation token\n")

	jwt, err := GenerateJWT(*clientID, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT: %w", err)
	}

	token, expiry, err := GetInstallationToken(jwt)
	if err != nil {
		return "", fmt.Errorf("failed to get installation token: %w", err)
	}

	installationToken = token
	installationTokenExpiry = expiry.Add(-(3 * time.Minute))

	log.Printf("installation token expires at %s\n", expiry)

	return installationToken, nil
}

// GenerateJWT creates a JWT for authenticating as a GitHub App.
func GenerateJWT(clientID string, privateKey *rsa.PrivateKey) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": clientID,
		"alg": "RS256",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(privateKey)
}

// GetInstallationToken fetches an installation token for the GitHub App.
func GetInstallationToken(jwt string) (string, time.Time, error) {
	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", *installationID)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to fetch installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("failed to get installation token: %s", resp.Status)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to parse response: %w", err)
	}
	return body.Token, time.Now().Add(time.Hour), nil
}

// GetFileContent retrieves the file content from the GitHub repository.
func GetFileContent(owner, repo, path, token string) ([]byte, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("failed to fetch file: %s", resp.Status)
	}

	var fileData struct {
		Content     string `json:"content"`
		Name        string `json:"name"`
		Encoding    string `json:"encoding"`
		Size        int64  `json:"size"`
		DownloadURL string `json:"download_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&fileData); err != nil {
		return nil, "", fmt.Errorf("failed to parse file data: %w", err)
	}

	ext := filepath.Ext(fileData.Name)
	var content []byte

	// For files larger than 1MB, use the download_url
	if fileData.Size > 1024*1024 {
		req, err = http.NewRequest("GET", fileData.DownloadURL, nil)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create download request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = client.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("failed to download file: %w", err)
		}
		defer resp.Body.Close()

		content, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read download response: %w", err)
		}
	} else {
		// Decode the Base64-encoded content
		content, err = base64.StdEncoding.DecodeString(fileData.Content)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode file content: %w", err)
		}
	}

	if lfsPointer, ok := parseLFSPointer(content); ok {
		content, err = downloadLFSObject(owner, repo, token, lfsPointer)
		if err != nil {
			return nil, "", fmt.Errorf("failed to download LFS object: %w", err)
		}
	}

	// Identify content type
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		mtype := mimetype.Detect(content)
		if mtype != nil {
			contentType = mtype.String()
		} else {
			contentType = "application/octet-stream" // Default fallback content type
		}
	}

	log.Printf("serving filename: %s, Size: %d bytes, File type: %v\n", fileData.Name, fileData.Size, contentType)

	return content, contentType, nil
}

type lfsPointer struct {
	OID  string
	Size int64
}

func parseLFSPointer(content []byte) (*lfsPointer, bool) {
	text := strings.TrimSpace(string(content))
	if !strings.HasPrefix(text, "version https://git-lfs.github.com/spec/v1") {
		return nil, false
	}

	var oid string
	var size int64
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "oid sha256:") {
			oid = strings.TrimPrefix(line, "oid sha256:")
			continue
		}
		if strings.HasPrefix(line, "size ") {
			val := strings.TrimPrefix(line, "size ")
			parsed, err := strconv.ParseInt(val, 10, 64)
			if err == nil {
				size = parsed
			}
		}
	}

	if oid == "" || size <= 0 {
		return nil, false
	}

	return &lfsPointer{OID: oid, Size: size}, true
}

type lfsBatchRequest struct {
	Operation string           `json:"operation"`
	Objects   []lfsBatchObject `json:"objects"`
}

type lfsBatchObject struct {
	OID  string `json:"oid"`
	Size int64  `json:"size"`
}

type lfsBatchResponse struct {
	Objects []lfsBatchResponseObject `json:"objects"`
}

type lfsBatchResponseObject struct {
	OID     string                    `json:"oid"`
	Size    int64                     `json:"size"`
	Actions map[string]lfsBatchAction `json:"actions"`
	Error   *lfsBatchError            `json:"error"`
}

type lfsBatchAction struct {
	Href   string            `json:"href"`
	Header map[string]string `json:"header"`
}

type lfsBatchError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func downloadLFSObject(owner, repo, token string, pointer *lfsPointer) ([]byte, error) {
	reqBody := lfsBatchRequest{
		Operation: "download",
		Objects:   []lfsBatchObject{{OID: pointer.OID, Size: pointer.Size}},
	}

	action, err := requestLFSBatchAction(fmt.Sprintf("https://api.github.com/repos/%s/%s/git/lfs/objects/batch", owner, repo), token, reqBody, false)
	if err != nil {
		if !strings.Contains(err.Error(), "404") {
			return nil, err
		}

		action, err = requestLFSBatchAction(fmt.Sprintf("https://github.com/%s/%s.git/info/lfs/objects/batch", owner, repo), token, reqBody, true)
		if err != nil {
			return nil, err
		}
	}

	return downloadLFSAction(action)
}

func requestLFSBatchAction(url, token string, reqBody lfsBatchRequest, useBasicAuth bool) (lfsBatchAction, error) {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return lfsBatchAction{}, fmt.Errorf("failed to encode LFS batch request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return lfsBatchAction{}, fmt.Errorf("failed to create LFS batch request: %w", err)
	}
	if useBasicAuth {
		creds := "x-access-token:" + token
		encoded := base64.StdEncoding.EncodeToString([]byte(creds))
		req.Header.Set("Authorization", "Basic "+encoded)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.git-lfs+json")
	req.Header.Set("Content-Type", "application/vnd.git-lfs+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return lfsBatchAction{}, fmt.Errorf("failed to request LFS batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return lfsBatchAction{}, fmt.Errorf("failed to request LFS batch: %s", resp.Status)
	}

	var batchResp lfsBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return lfsBatchAction{}, fmt.Errorf("failed to parse LFS batch response: %w", err)
	}

	if len(batchResp.Objects) == 0 {
		return lfsBatchAction{}, fmt.Errorf("LFS batch response missing objects")
	}

	obj := batchResp.Objects[0]
	if obj.Error != nil {
		return lfsBatchAction{}, fmt.Errorf("LFS error %d: %s", obj.Error.Code, obj.Error.Message)
	}

	action, ok := obj.Actions["download"]
	if !ok || action.Href == "" {
		return lfsBatchAction{}, fmt.Errorf("LFS download action missing")
	}

	return action, nil
}

func downloadLFSAction(action lfsBatchAction) ([]byte, error) {
	client := &http.Client{}
	downloadReq, err := http.NewRequest("GET", action.Href, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create LFS download request: %w", err)
	}
	for key, value := range action.Header {
		downloadReq.Header.Set(key, value)
	}

	downloadResp, err := client.Do(downloadReq)
	if err != nil {
		return nil, fmt.Errorf("failed to download LFS object: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download LFS object: %s", downloadResp.Status)
	}

	content, err := io.ReadAll(downloadResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read LFS download response: %w", err)
	}

	return content, nil
}

type RateLimit struct {
	Resources struct {
		Core struct {
			Limit     int `json:"limit"`
			Remaining int `json:"remaining"`
			Reset     int `json:"reset"`
		} `json:"core"`
	} `json:"resources"`
}

// fetchRateLimit fetches the rate limit for the GitHub API.
func fetchRateLimit(token string) (*RateLimit, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/rate_limit", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rate limit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch rate limit: %s", resp.Status)
	}

	var rateLimit RateLimit
	if err := json.NewDecoder(resp.Body).Decode(&rateLimit); err != nil {
		return nil, fmt.Errorf("failed to parse rate limit response: %w", err)
	}

	return &rateLimit, nil
}
