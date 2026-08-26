// Package store persists completed crawl jobs to MongoDB so past reports
// survive process restarts and can be listed on the Reports page.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"seo-crawler/internal/models"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const dbTimeout = 10 * time.Second

type Store struct {
	client  *mongo.Client
	reports *mongo.Collection
	users   *mongo.Collection
}

// reportDoc is the on-disk shape of one report in the "reports" collection.
// Results are kept as a single marshaled JSON blob (mirroring the previous
// SQLite column) rather than a native BSON array, since nothing queries into
// individual result fields.
type reportDoc struct {
	JobID       string     `bson:"_id"`
	UserID      string     `bson:"user_id"`
	Domain      string     `bson:"domain"`
	CreatedAt   time.Time  `bson:"created_at"`
	CompletedAt *time.Time `bson:"completed_at,omitempty"`
	PagesTotal  int        `bson:"pages_total"`
	AvgScore    int        `bson:"avg_score"`
	SitemapURL  string     `bson:"sitemap_url"`
	ResultsJSON string     `bson:"results_json"`
}

// New initializes the Store using an existing connected MongoDB client
// and ensures the collections' indexes exist.
func New(client *mongo.Client) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	reports := client.Database("auditly").Collection("reports")
	// Index on created_at (for default sort)
	if _, err := reports.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "created_at", Value: -1}},
	}); err != nil {
		return nil, fmt.Errorf("create index: %w", err)
	}
	// Index on user_id (for per-user report listing)
	if _, err := reports.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
	}); err != nil {
		return nil, fmt.Errorf("create user_id index: %w", err)
	}

	users := client.Database("auditly").Collection("users")
	if _, err := users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return nil, fmt.Errorf("create users email index: %w", err)
	}

	return &Store{client: client, reports: reports, users: users}, nil
}

// ReportSummary is one row in the Reports list table.
type ReportSummary struct {
	JobID       string     `json:"job_id"`
	UserID      string     `json:"user_id,omitempty"`
	Domain      string     `json:"domain"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	PagesTotal  int        `json:"pages_total"`
	AvgScore    int        `json:"avg_score"`
}

// SaveReport persists a completed job. Safe to call multiple times for the
// same job ID (e.g. re-saving) — the document is replaced.
func (s *Store) SaveReport(job *models.Job) error {
	resultsJSON, err := json.Marshal(job.Results)
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}

	doc := reportDoc{
		JobID:       job.ID,
		UserID:      job.UserID,
		Domain:      job.Domain,
		CreatedAt:   job.CreatedAt,
		CompletedAt: job.CompletedAt,
		PagesTotal:  len(job.Results),
		AvgScore:    averageScore(job.Results),
		SitemapURL:  job.SitemapURL,
		ResultsJSON: string(resultsJSON),
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	_, err = s.reports.ReplaceOne(ctx, bson.M{"_id": job.ID}, doc, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save report: %w", err)
	}
	return nil
}

// ListReports returns saved reports, most recent first.
// If userID is non-empty, only that user's reports are returned.
// If userID is empty (guest), an empty list is returned.
func (s *Store) ListReports(userID string) ([]ReportSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	filter := bson.M{"user_id": userID}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetProjection(bson.M{"results_json": 0})
	cur, err := s.reports.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer cur.Close(ctx)

	var out []ReportSummary
	for cur.Next(ctx) {
		var d reportDoc
		if err := cur.Decode(&d); err != nil {
			return nil, fmt.Errorf("scan report: %w", err)
		}
		out = append(out, ReportSummary{
			JobID:       d.JobID,
			UserID:      d.UserID,
			Domain:      d.Domain,
			CreatedAt:   d.CreatedAt,
			CompletedAt: d.CompletedAt,
			PagesTotal:  d.PagesTotal,
			AvgScore:    d.AvgScore,
		})
	}
	return out, cur.Err()
}

// GetResults returns the stored page results for a job ID, or (nil, false) if
// no report with that ID has been saved.
func (s *Store) GetResults(jobID string) ([]models.SEOResult, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var d reportDoc
	err := s.reports.FindOne(ctx, bson.M{"_id": jobID}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get results: %w", err)
	}

	var results []models.SEOResult
	if err := json.Unmarshal([]byte(d.ResultsJSON), &results); err != nil {
		return nil, false, fmt.Errorf("unmarshal results: %w", err)
	}
	return results, true, nil
}

func averageScore(results []models.SEOResult) int {
	if len(results) == 0 {
		return 0
	}
	total := 0
	for _, r := range results {
		total += r.Score
	}
	return total / len(results)
}

// CreateUser inserts a new user document. Returns an error if email is already taken.
func (s *Store) CreateUser(user *models.User) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	if user.ID == "" {
		user.ID = bson.NewObjectID().Hex()
	}

	_, err := s.users.InsertOne(ctx, user)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// GetUserByEmail retrieves a user by their email address.
func (s *Store) GetUserByEmail(email string) (*models.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var user models.User
	err := s.users.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err == mongo.ErrNoDocuments {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &user, nil
}
