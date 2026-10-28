package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
)

func TestClaimSkipsLeasedJob(t *testing.T) {
	for _, part := range []string{"FOR UPDATE SKIP LOCKED", "lease_until IS NULL OR lease_until < now()", "next_run_at <= now()"} {
		if !strings.Contains(claimSelectSQL, part) {
			t.Fatalf("claim SQL lacks %q", part)
		}
	}
	db := newPool(t)
	db.ExpectBegin()
	db.ExpectQuery(claimSelectSQL).WillReturnRows(pgxmock.NewRows([]string{"id", "session_id", "kind", "attempt"}).AddRow(int64(5), testID, "create", 2))
	db.ExpectExec(claimUpdateSQL).WithArgs(int64(5), "w1", 30.0).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	db.ExpectCommit()
	db.ExpectRollback()
	job, ok, err := NewRepository(db).Claim(context.Background(), "w1", 30*time.Second)
	if err != nil || !ok || job.ID != 5 || job.Kind != usecase.JobCreate || job.Attempt != 3 || job.SessionID != testID {
		t.Fatalf("job=%+v ok=%v err=%v", job, ok, err)
	}
}

func TestClaimNothingReady(t *testing.T) {
	db := newPool(t)
	db.ExpectBegin()
	db.ExpectQuery(claimSelectSQL).WillReturnError(pgx.ErrNoRows)
	db.ExpectRollback()
	_, ok, err := NewRepository(db).Claim(context.Background(), "w1", time.Second)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRenewReturnsCurrentKind(t *testing.T) {
	db := newPool(t)
	db.ExpectQuery(renewSQL).WithArgs(int64(5), "w1", 10.0).WillReturnRows(pgxmock.NewRows([]string{"kind"}).AddRow("delete"))
	kind, err := NewRepository(db).Renew(context.Background(), usecase.Job{ID: 5}, "w1", 10*time.Second)
	if err != nil || kind != usecase.JobDelete {
		t.Fatalf("kind=%v err=%v", kind, err)
	}
}

func TestRenewLeaseLost(t *testing.T) {
	db := newPool(t)
	db.ExpectQuery(renewSQL).WithArgs(int64(5), "w1", 10.0).WillReturnError(pgx.ErrNoRows)
	_, err := NewRepository(db).Renew(context.Background(), usecase.Job{ID: 5}, "w1", 10*time.Second)
	if !errors.Is(err, usecase.ErrLeaseLost) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompleteAndRetryLeaseLost(t *testing.T) {
	at := time.Now()
	db := newPool(t)
	db.ExpectExec(completeSQL).WithArgs(int64(5), "w1", "create").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	db.ExpectExec(completeSQL).WithArgs(int64(5), "w1", "create").WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	db.ExpectExec(retrySQL).WithArgs(int64(5), "w1", at).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	db.ExpectExec(retrySQL).WithArgs(int64(5), "w1", at).WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	r := NewRepository(db)
	ctx := context.Background()
	if err := r.Complete(ctx, usecase.Job{ID: 5, Kind: usecase.JobCreate}, "w1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Complete(ctx, usecase.Job{ID: 5, Kind: usecase.JobCreate}, "w1"); !errors.Is(err, usecase.ErrLeaseLost) {
		t.Fatal(err)
	}
	if err := r.Retry(ctx, usecase.Job{ID: 5}, "w1", at); err != nil {
		t.Fatal(err)
	}
	if err := r.Retry(ctx, usecase.Job{ID: 5}, "w1", at); !errors.Is(err, usecase.ErrLeaseLost) {
		t.Fatal(err)
	}
}

func TestCompleteConvertedJobLeaseLost(t *testing.T) {
	if !strings.Contains(completeSQL, "kind = $3") {
		t.Fatal("complete SQL must match job kind")
	}
	db := newPool(t)
	db.ExpectExec(completeSQL).WithArgs(int64(5), "w1", "create").WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	err := NewRepository(db).Complete(context.Background(), usecase.Job{ID: 5, Kind: usecase.JobCreate}, "w1")
	if !errors.Is(err, usecase.ErrLeaseLost) {
		t.Fatalf("err=%v", err)
	}
}
