package privacy

import (
	"testing"
	"time"
)

func TestDailyHasherRotatesDaily(t *testing.T) {
	currentTime := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	hasher := &DailyHasher{
		nowFunc: func() time.Time { return currentTime },
	}

	h1 := hasher.HashVisitor("etin.dev", "203.0.113.42", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	h2 := hasher.HashVisitor("etin.dev", "203.0.113.42", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	if h1 != h2 {
		t.Fatalf("expected same hash within the same UTC day, got %s vs %s", h1, h2)
	}

	// Advance to the next UTC day: salt must rotate and produce a completely different hash
	currentTime = currentTime.Add(24 * time.Hour)
	h3 := hasher.HashVisitor("etin.dev", "203.0.113.42", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	if h1 == h3 {
		t.Fatalf("expected hash to change across UTC days, but both were %s", h1)
	}
}

func TestIsBot(t *testing.T) {
	if !IsBot("Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)") {
		t.Fatal("expected Googlebot to be detected as bot")
	}
	if !IsBot("") {
		t.Fatal("expected empty UA to be treated as bot")
	}
	if IsBot("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15") {
		t.Fatal("expected real Safari UA not to be flagged as bot")
	}
}

func TestParseUserAgent(t *testing.T) {
	device, browser, osName := ParseUserAgent("Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1")
	if device != "mobile" || browser != "Safari" || osName != "iOS" {
		t.Fatalf("unexpected parse result: device=%s browser=%s os=%s", device, browser, osName)
	}
}

func TestSanitizePathAndReferrer(t *testing.T) {
	if got := SanitizePath("/notes/my-post/?utm_source=twitter#section"); got != "/notes/my-post" {
		t.Fatalf("expected /notes/my-post, got %s", got)
	}
	if got := SanitizeReferrer("https://www.etin.dev/notes", "etin.dev"); got != "Direct" {
		t.Fatalf("expected self-referral to become Direct, got %s", got)
	}
	if got := SanitizeReferrer("https://news.ycombinator.com/item?id=123", "etin.dev"); got != "news.ycombinator.com" {
		t.Fatalf("expected news.ycombinator.com, got %s", got)
	}
}
