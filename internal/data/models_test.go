package data

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRecordPageviewAndEngagement(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	store := NewAnalyticsStore(db)

	mock.ExpectExec("INSERT INTO pageviews").
		WithArgs(
			"11111111-1111-1111-1111-111111111111",
			"etin.dev",
			"/notes/architecture",
			"abcd1234",
			nil,
			false,
			"github.com",
			"",
			"",
			"",
			"mobile",
			"Safari",
			"iOS",
			"NG",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = store.RecordPageview(context.Background(), PageviewInput{
		ID:             "11111111-1111-1111-1111-111111111111",
		SiteID:         "etin.dev",
		Path:           "/notes/architecture",
		VisitorHash:    "abcd1234",
		ReferrerDomain: "github.com",
		DeviceType:     "mobile",
		Browser:        "Safari",
		OS:             "iOS",
		Country:        "NG",
	})
	if err != nil {
		t.Fatalf("RecordPageview failed: %v", err)
	}

	mock.ExpectExec("UPDATE pageviews").
		WithArgs("11111111-1111-1111-1111-111111111111", 45, 75).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = store.UpdateEngagement(context.Background(), EngagementUpdate{
		PageviewID:      "11111111-1111-1111-1111-111111111111",
		DurationSeconds: 45,
		MaxScrollDepth:  75,
	})
	if err != nil {
		t.Fatalf("UpdateEngagement failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled sql expectations: %v", err)
	}
}
