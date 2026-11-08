package main

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tokenVerifier struct {
	mu           sync.RWMutex
	validUntil   map[string]time.Time
	cacheTTL     time.Duration
	apiVerifyURL string
	staticAPIKey string
	httpClient   *http.Client
}

func newTokenVerifier(apiBaseURL, staticAPIKey string, client *http.Client) *tokenVerifier {
	trimmedBase := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if trimmedBase == "" {
		trimmedBase = "https://api.etin.dev"
	}
	// Strip trailing /v1 if supplied so we always hit /v1/admin/verify cleanly
	trimmedBase = strings.TrimSuffix(trimmedBase, "/v1")

	return &tokenVerifier{
		validUntil:   make(map[string]time.Time),
		cacheTTL:     5 * time.Minute,
		apiVerifyURL: trimmedBase + "/v1/admin/verify",
		staticAPIKey: strings.TrimSpace(staticAPIKey),
		httpClient:   client,
	}
}

func (tv *tokenVerifier) validate(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	// 1. Check optional static API key fallback first
	if tv.staticAPIKey != "" && len(token) == len(tv.staticAPIKey) {
		if subtle.ConstantTimeCompare([]byte(token), []byte(tv.staticAPIKey)) == 1 {
			return true
		}
	}

	// 2. Check in-memory cache of verified admin.etin.dev tokens
	now := time.Now()
	tv.mu.RLock()
	expiry, found := tv.validUntil[token]
	tv.mu.RUnlock()
	if found && now.Before(expiry) {
		return true
	}

	// 3. Verify token against api.etin.dev
	req, err := http.NewRequest(http.MethodGet, tv.apiVerifyURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := tv.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		tv.mu.Lock()
		tv.validUntil[token] = now.Add(tv.cacheTTL)
		tv.mu.Unlock()
		return true
	}

	return false
}

func parseBearerToken(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", errors.New("missing authorization header")
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("authorization header is not a bearer token")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errors.New("authorization token missing")
	}
	return token, nil
}
