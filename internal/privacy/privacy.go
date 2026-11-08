package privacy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DailyHasher computes an anonymous, irreversible daily visitor hash using a
// secret salt that automatically rotates every UTC calendar day. Old salts are
// overwritten in memory and never persisted, ensuring visitor hashes cannot be
// reversed to an IP address or correlated across days.
type DailyHasher struct {
	mu      sync.Mutex
	dayKey  string
	salt    []byte
	nowFunc func() time.Time
}

// NewDailyHasher creates a DailyHasher using UTC time.
func NewDailyHasher() *DailyHasher {
	return &DailyHasher{
		nowFunc: func() time.Time { return time.Now().UTC() },
	}
}

func (h *DailyHasher) currentSalt() ([]byte, string) {
	now := h.nowFunc().UTC()
	today := now.Format("2006-01-02")

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.dayKey != today || len(h.salt) == 0 {
		newSalt := make([]byte, 32)
		if _, err := rand.Read(newSalt); err != nil {
			// Fallback to deterministic entropy from timestamp if OS RNG fails
			sum := sha256.Sum256([]byte(now.String()))
			newSalt = sum[:]
		}
		h.salt = newSalt
		h.dayKey = today
	}

	saltCopy := make([]byte, len(h.salt))
	copy(saltCopy, h.salt)
	return saltCopy, h.dayKey
}

// HashVisitor returns a 64-character hex SHA-256 digest of:
// daily_salt + "|" + day + "|" + siteID + "|" + clientIP + "|" + userAgent.
// Raw IP addresses are never stored on disk or in the database.
func (h *DailyHasher) HashVisitor(siteID, clientIP, userAgent string) string {
	salt, day := h.currentSalt()
	mac := sha256.New()
	mac.Write(salt)
	mac.Write([]byte("|"))
	mac.Write([]byte(day))
	mac.Write([]byte("|"))
	mac.Write([]byte(strings.TrimSpace(siteID)))
	mac.Write([]byte("|"))
	mac.Write([]byte(strings.TrimSpace(clientIP)))
	mac.Write([]byte("|"))
	mac.Write([]byte(strings.TrimSpace(userAgent)))
	return hex.EncodeToString(mac.Sum(nil))
}

// ExtractClientIP extracts the client IP from reverse-proxy headers or RemoteAddr
// strictly in memory for computing the ephemeral daily visitor hash.
func ExtractClientIP(r *http.Request) string {
	if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
		return cfIP
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if first := strings.TrimSpace(parts[0]); first != "" {
			return first
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

// ExtractCountry reads a 2-letter ISO country code from CDN headers if present.
func ExtractCountry(r *http.Request) string {
	for _, header := range []string{"CF-IPCountry", "X-Country-Code", "X-Vercel-IP-Country", "X-Nf-Country-Code"} {
		val := strings.ToUpper(strings.TrimSpace(r.Header.Get(header)))
		if len(val) == 2 && val != "XX" && val != "T1" {
			return val
		}
	}
	return "UN"
}

var botSubstrings = []string{
	"bot", "crawler", "spider", "slurp", "headless", "lighthouse",
	"pagespeed", "pingdom", "uptimerobot", "statuscake", "datadog",
	"semrush", "ahrefs", "mj12bot", "dotbot", "petalbot", "bytespider",
	"facebookexternalhit", "twitterbot", "linkedinbot", "discordbot",
	"whatsapp", "telegrambot", "curl/", "wget/", "python-requests",
	"go-http-client", "axios/", "playwright", "puppeteer", "cypress",
}

// IsBot returns true if the User-Agent matches known bots, crawlers, or synthetic monitors.
func IsBot(userAgent string) bool {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	if ua == "" {
		return true
	}
	for _, token := range botSubstrings {
		if strings.Contains(ua, token) {
			return true
		}
	}
	return false
}

// ParseUserAgent extracts coarse, privacy-safe device, browser, and OS labels
// without storing raw User-Agent strings or browser version numbers.
func ParseUserAgent(userAgent string) (deviceType, browser, osName string) {
	ua := strings.ToLower(userAgent)

	// Device Type
	switch {
	case strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet") || (strings.Contains(ua, "android") && !strings.Contains(ua, "mobile")):
		deviceType = "tablet"
	case strings.Contains(ua, "mobi") || strings.Contains(ua, "iphone") || strings.Contains(ua, "ipod") || strings.Contains(ua, "android"):
		deviceType = "mobile"
	default:
		deviceType = "desktop"
	}

	// Browser (order matters since Edge/Chrome/Safari share tokens)
	switch {
	case strings.Contains(ua, "edg/") || strings.Contains(ua, "edgios") || strings.Contains(ua, "edga"):
		browser = "Edge"
	case strings.Contains(ua, "opr/") || strings.Contains(ua, "opera"):
		browser = "Opera"
	case strings.Contains(ua, "firefox/") || strings.Contains(ua, "fxios"):
		browser = "Firefox"
	case strings.Contains(ua, "chrome/") || strings.Contains(ua, "crios"):
		browser = "Chrome"
	case strings.Contains(ua, "safari/"):
		browser = "Safari"
	default:
		browser = "Other"
	}

	// Operating System
	switch {
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod") || strings.Contains(ua, "cpu os"):
		osName = "iOS"
	case strings.Contains(ua, "android"):
		osName = "Android"
	case strings.Contains(ua, "mac os x") || strings.Contains(ua, "macintosh"):
		osName = "macOS"
	case strings.Contains(ua, "windows"):
		osName = "Windows"
	case strings.Contains(ua, "linux"):
		osName = "Linux"
	default:
		osName = "Other"
	}

	return deviceType, browser, osName
}

// SanitizePath normalizes a URL or path into a clean path without query strings or fragments.
func SanitizePath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "/"
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
		trimmed = parsed.Path
	} else if idx := strings.IndexAny(trimmed, "?#"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	if len(trimmed) > 1 {
		trimmed = strings.TrimRight(trimmed, "/")
	}
	if len(trimmed) > 512 {
		trimmed = trimmed[:512]
	}
	return trimmed
}

// SanitizeReferrer extracts the hostname of an external referrer, returning "Direct"
// for empty or self-referrals.
func SanitizeReferrer(rawReferrer string, ownDomains ...string) string {
	trimmed := strings.TrimSpace(rawReferrer)
	if trimmed == "" {
		return "Direct"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return "Direct"
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return "Direct"
	}
	for _, domain := range ownDomains {
		cleanDomain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "www.")
		if cleanDomain != "" && (host == cleanDomain || strings.HasSuffix(host, "."+cleanDomain)) {
			return "Direct"
		}
	}
	return host
}
