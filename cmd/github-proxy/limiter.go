package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	ClientRate  time.Duration = time.Minute / 60
	ClientBurst int           = 8
)

var (
	globalLimiter  *rate.Limiter
	clientLimiters = make(map[string]*clientLimiter)
	limiterMutex   sync.Mutex
)

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// checkLimits checks the request against current global and client rate limits
func checkLimits(r *http.Request) error {
	if !globalLimiter.Allow() {
		return fmt.Errorf("global rate limit exceeded")
	}

	clientIP := getClientIP(r)
	clientLimiter := getClientLimiter(clientIP)

	if !clientLimiter.Allow() {
		log.Printf("client %s rate set to %d requests per minute with burst: %d\n", clientIP, int(time.Minute/ClientRate), ClientBurst)
		return fmt.Errorf("client rate limit exceeded")
	}

	return nil
}

// getClientIP returns the client IP address from the request
func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		ips := strings.Split(ip, ",")
		return strings.TrimSpace(ips[0])
	}

	pos := strings.LastIndex(r.RemoteAddr, ":")
	if pos == -1 {
		return r.RemoteAddr
	}
	return r.RemoteAddr[:pos]
}

// getClientLimiter returns a rate limiter for the given client IP address
func getClientLimiter(ip string) *rate.Limiter {
	limiterMutex.Lock()
	defer limiterMutex.Unlock()

	if l, ok := clientLimiters[ip]; ok {
		l.lastSeen = time.Now()
		return l.limiter
	}

	// rate limit a client to 60 requests per minute, with a burst of 10
	l := rate.NewLimiter(rate.Every(ClientRate), ClientBurst)
	clientLimiters[ip] = &clientLimiter{l, time.Now()}
	log.Printf("client %s rate set to %d requests per minute with burst: %d\n", ip, int(time.Minute/ClientRate), ClientBurst)

	return l
}

// cleanupStaleLimiters periodically removes stale client limiters
func cleanupStaleLimiters(ctx context.Context, duration time.Duration) {
	deleteStaleLimiters := func(duration time.Duration) {
		limiterMutex.Lock()

		for ip, l := range clientLimiters {
			if time.Since(l.lastSeen) > duration {
				delete(clientLimiters, ip)
			}
		}

		limiterMutex.Unlock()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(duration):
			deleteStaleLimiters(duration)
		}
	}
}

func initGlobalLimiter(token string) error {
	rateLimit, err := fetchRateLimit(token)
	if err != nil {
		return err
	}

	limit := rateLimit.Resources.Core.Limit
	reset := time.Unix(int64(rateLimit.Resources.Core.Reset), 0)
	duration := time.Until(reset)

	globalLimiter = rate.NewLimiter(rate.Every(duration/time.Duration(limit)), limit)
	log.Printf("global rate limit set to %d requests per hour\n", limit)

	return nil
}
