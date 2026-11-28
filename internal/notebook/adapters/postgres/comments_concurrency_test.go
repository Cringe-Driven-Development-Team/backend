package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/testutil/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func commentNotebook(t *testing.T) (*NotebookRepository, *pgxpool.Pool, domain.Notebook, domain.Comment) {
	t.Helper()
	repo, pool := testRepository(t)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `INSERT INTO identity.users(id,login,password_hash) OVERRIDING SYSTEM VALUE VALUES(2,'reader','hash')`); err != nil {
		t.Fatal(err)
	}
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	n, err := uc.Create(ctx, 1, "wall")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.PutSharing(ctx, n.ID, 1, false, []domain.NotebookReader{{ID: 2, Role: domain.RoleCommenter}}); err != nil {
		t.Fatal(err)
	}
	c, err := repo.CreateComment(ctx, n.ID, n.Cells[0].ID, 2, "before")
	if err != nil {
		t.Fatal(err)
	}
	return repo, pool, n, c.Comment
}

func TestCellCommentsSerializeWithAccessAndDeletion(t *testing.T) {
	for _, method := range []string{"POST", "PATCH", "DELETE", "GET"} {
		for _, changeKind := range []string{"revoke", "cell", "notebook"} {
			for _, writeFirst := range []bool{false, true} {
				name := method + "/" + changeKind + map[bool]string{false: "/change-first", true: "/write-first"}[writeFirst]
				t.Run(name, func(t *testing.T) {
					_, pool, n, c := commentNotebook(t)
					ctx := t.Context()
					cell := n.Cells[0].ID
					write := func(r *NotebookRepository) error {
						switch method {
						case "POST":
							_, e := r.CreateComment(ctx, n.ID, cell, 2, "after")
							return e
						case "PATCH":
							_, e := r.UpdateComment(ctx, n.ID, cell, 2, c.ID, "after")
							return e
						case "DELETE":
							return r.DeleteComment(ctx, n.ID, cell, 2, c.ID)
						default:
							_, e := r.ListComments(ctx, n.ID, cell, 2, usecase.CommentQuery{Limit: 20})
							return e
						}
					}
					change := func(r *NotebookRepository) error {
						switch changeKind {
						case "revoke":
							_, e := r.PutSharing(ctx, n.ID, 1, false, []domain.NotebookReader{})
							return e
						case "cell":
							_, e := r.Apply(ctx, n.ID, 1, n.Revision, usecase.Operation{Kind: usecase.Delete, CellID: cell})
							return e
						default:
							return r.DeleteNotebook(ctx, n.ID, 1)
						}
					}
					first, second := change, write
					if writeFirst {
						first, second = write, change
					}
					e1, e2 := serializeRename(t, pool, first, second)
					if e1 != nil {
						t.Fatal(e1)
					}
					if writeFirst {
						if e2 != nil {
							t.Fatal(e2)
						}
					} else if !errors.Is(e2, usecase.ErrNotebookNotFound) && !errors.Is(e2, usecase.ErrCellNotFound) {
						t.Fatalf("write after access/deletion: %v", e2)
					}
					if changeKind != "revoke" {
						var count int
						if e := pool.QueryRow(ctx, `SELECT count(*) FROM workspace.cell_comments WHERE notebook_id=$1`, n.ID).Scan(&count); e != nil || count != 0 {
							t.Fatalf("cascade %d %v", count, e)
						}
					}
				})
			}
		}
	}
}

func TestCellCommentsConcurrentPatches(t *testing.T) {
	_, pool, n, c := commentNotebook(t)
	var one, two usecase.CommentRecord
	first := func(r *NotebookRepository) error {
		var e error
		one, e = r.UpdateComment(t.Context(), n.ID, c.CellID, 2, c.ID, "one")
		return e
	}
	second := func(r *NotebookRepository) error {
		var e error
		two, e = r.UpdateComment(t.Context(), n.ID, c.CellID, 2, c.ID, "two")
		return e
	}
	e1, e2 := serializeRename(t, pool, first, second)
	if e1 != nil || e2 != nil || !two.Comment.UpdatedAt.After(one.Comment.UpdatedAt) {
		t.Fatal(one, two, e1, e2)
	}
	var text string
	if err := pool.QueryRow(t.Context(), `SELECT text FROM workspace.cell_comments WHERE id=$1`, c.ID).Scan(&text); err != nil || text != "two" {
		t.Fatal(text, err)
	}
}

func TestCellCommentsCanceledLockWait(t *testing.T) {
	_, pool, n, c := commentNotebook(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err = blocker.Exec(ctx, `SELECT id FROM workspace.notebooks WHERE id=$1 FOR UPDATE`, n.ID); err != nil {
		t.Fatal(err)
	}
	waitCtx, stop := context.WithCancel(ctx)
	defer stop()
	gate := lockDB{DB: pool, waiter: make(chan uint32, 1)}
	done := make(chan error, 1)
	go func() {
		_, e := NewNotebookRepository(gate).CreateComment(waitCtx, n.ID, c.CellID, 2, "canceled")
		done <- e
	}()
	pid := awaitRename(ctx, t, gate.waiter)
	awaitNotebookLock(ctx, t, pool, pid)
	stop()
	if e := awaitRename(ctx, t, done); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM workspace.cell_comments WHERE notebook_id=$1`, n.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = NewNotebookRepository(pool).CreateComment(ctx, n.ID, c.CellID, 2, "next"); err != nil {
		t.Fatal(err)
	}
}

var errCommentInjected = errors.New("injected comment transaction failure")

type commentFaultDB struct {
	DB
	readAt int
	commit bool
}

// Begin оборачивает настоящую транзакцию для проверки атомарности при отказах.
//
//nolint:ireturn // Реализует существующий порт DB.
func (d commentFaultDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.DB.Begin(ctx)
	return &commentFaultTx{Tx: tx, readAt: d.readAt, commit: d.commit}, err
}

type commentFaultTx struct {
	pgx.Tx
	reads, readAt int
	commit        bool
}

// QueryRow внедряет отказ после заданного чтения комментария, не подменяя DML.
//
//nolint:ireturn // Реализует pgx.Tx.
func (tx *commentFaultTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "FROM workspace.cell_comments") {
		tx.reads++
		if tx.reads == tx.readAt {
			return commentErrorRow{}
		}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

// Commit имитирует известный отказ с подтверждённым откатом настоящей транзакции.
func (tx *commentFaultTx) Commit(ctx context.Context) error {
	if tx.commit {
		_ = tx.Rollback(ctx)
		return errCommentInjected
	}
	return tx.Tx.Commit(ctx)
}

type commentErrorRow struct{}

// Scan возвращает точечную ошибку вместо данных SELECT.
func (commentErrorRow) Scan(...any) error { return errCommentInjected }

func TestCellCommentsRollback(t *testing.T) {
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		for _, commit := range []bool{false, true} {
			if method == "DELETE" && !commit {
				continue
			}
			t.Run(method+map[bool]string{false: "/read", true: "/commit"}[commit], func(t *testing.T) {
				repo, pool, n, c := commentNotebook(t)
				readAt := 1
				if method == "PATCH" {
					readAt = 2
				}
				if commit {
					readAt = 0
				}
				faulty := NewNotebookRepository(commentFaultDB{DB: pool, readAt: readAt, commit: commit})
				var err error
				switch method {
				case "POST":
					_, err = faulty.CreateComment(t.Context(), n.ID, c.CellID, 2, "after")
				case "PATCH":
					_, err = faulty.UpdateComment(t.Context(), n.ID, c.CellID, 2, c.ID, "after")
				default:
					err = faulty.DeleteComment(t.Context(), n.ID, c.CellID, 2, c.ID)
				}
				if !errors.Is(err, errCommentInjected) {
					t.Fatal(err)
				}
				page, err := repo.ListComments(t.Context(), n.ID, c.CellID, 2, usecase.CommentQuery{Limit: 20})
				if err != nil || len(page.Items) != 1 || page.Items[0].Text != c.Text || !page.Items[0].CreatedAt.Equal(c.CreatedAt) || !page.Items[0].UpdatedAt.Equal(c.UpdatedAt) {
					t.Fatal(page, err)
				}
			})
		}
	}
}
