package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v5"
)

func TestCommentRepositoryTransactionOrder(t *testing.T) {
	failure := errors.New("injected failure")
	for _, stage := range []string{"begin", "lock", "access", "cell", "insert", "read", "commit", "success"} {
		t.Run(stage, func(t *testing.T) {
			db, err := pgxmock.NewPool()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			begin := db.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				db.ExpectExec(`INSERT INTO workspace.user_guards`).WithArgs(int64(2)).WillReturnResult(pgxmock.NewResult("INSERT", 1))
				db.ExpectQuery(`SELECT deleting,active_session_id FROM workspace.user_guards`).WithArgs(int64(2)).WillReturnRows(pgxmock.NewRows([]string{"deleting", "active_session_id"}).AddRow(false, nil))
				lock := db.ExpectQuery(`SELECT owner_id FROM workspace.notebooks WHERE id=\$1 FOR UPDATE`).WithArgs(int64(7))
				if stage == "lock" {
					lock.WillReturnError(failure)
				} else {
					lock.WillReturnRows(pgxmock.NewRows([]string{"owner_id"}).AddRow(int64(1)))
					access := db.ExpectQuery(`SELECT id FROM workspace.notebooks WHERE id=\$1 AND`).WithArgs(int64(7), int64(2))
					if stage == "access" {
						access.WillReturnError(failure)
					} else {
						access.WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(7)))
						cell := db.ExpectQuery(`SELECT cell_id FROM workspace.notebook_cells`).WithArgs(int64(7), "cell")
						if stage == "cell" {
							cell.WillReturnError(failure)
						} else {
							cell.WillReturnRows(pgxmock.NewRows([]string{"cell_id"}).AddRow("cell"))
							insert := db.ExpectQuery(`INSERT INTO workspace.cell_comments`).WithArgs(int64(7), "cell", int64(2), "text")
							if stage == "insert" {
								insert.WillReturnError(failure)
							} else {
								insert.WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(9)))
								read := db.ExpectQuery(`SELECT .* FROM workspace.cell_comments c`).WithArgs(int64(7), "cell", int64(9))
								if stage == "read" {
									read.WillReturnError(failure)
								} else {
									now := time.Now()
									read.WillReturnRows(pgxmock.NewRows([]string{"id", "book", "cell", "author", "text", "created", "updated"}).AddRow(int64(9), int64(7), "cell", int64(2), "text", now, now))
									commit := db.ExpectCommit()
									if stage == "commit" {
										commit.WillReturnError(failure)
									}
								}
							}
						}
					}
				}
				db.ExpectRollback()
			}
			got, err := NewNotebookRepository(db).CreateComment(t.Context(), 7, "cell", 2, "text")
			if stage == "success" {
				if err != nil || got.Comment.ID != 9 {
					t.Fatal(got, err)
				}
			} else if !errors.Is(err, failure) || got.Comment.ID != 0 {
				t.Fatalf("stage %s: %+v %v", stage, got, err)
			}
			if err = db.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
