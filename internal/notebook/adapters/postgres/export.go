package postgres

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/ipynb"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

// ExportSnapshot закрепляет права и ревизию в одной read-only транзакции до создания файла.
func (r *NotebookRepository) ExportSnapshot(ctx context.Context, id, visitor int64, render func(domain.Document, func(func(domain.Cell) error) error) error) (domain.Notebook, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Notebook{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return domain.Notebook{}, err
	}
	n, err := scanNotebook(tx.QueryRow(ctx, selectReadableNotebook, id, visitor))
	if err != nil {
		return n, err
	}
	err = tx.QueryRow(ctx, `SELECT nbformat,nbformat_minor,metadata,extra FROM workspace.notebooks WHERE id=$1`, n.ID).
		Scan(&n.Document.NBFormat, &n.Document.NBFormatMinor, &n.Document.Metadata, &n.Document.Extra)
	if err != nil {
		return n, err
	}
	if err = ipynb.NormalizeFields(&n.Document, nil); err != nil {
		return n, err
	}
	err = render(n.Document, func(write func(domain.Cell) error) error {
		rows, queryErr := tx.Query(ctx, `SELECT c.cell_id,c.kind,c.source,c.source_version,c.metadata,c.extra,d.outputs,
			d.execution_count,d.has_execution_count,d.attachments,d.output_truncated,d.has_runtime_metadata
			FROM workspace.notebook_cells c JOIN workspace.notebook_cell_data d USING(notebook_id,cell_id)
			WHERE c.notebook_id=$1 ORDER BY c.position`, n.ID)
		if queryErr != nil {
			return queryErr
		}
		defer rows.Close()
		for rows.Next() {
			cells := make([]domain.Cell, 1)
			c := &cells[0]
			if err = rows.Scan(&c.ID, &c.Kind, &c.Source, &c.SourceVersion, &c.Metadata, &c.Extra, &c.Outputs, &c.ExecutionCount,
				&c.HasExecutionCount, &c.Attachments, &c.OutputTruncated, &c.HasRuntimeMetadata); err != nil {
				return err
			}
			if err = ipynb.NormalizeFields(&domain.Document{NBFormat: 4}, cells); err != nil {
				return err
			}
			if err = write(*c); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return n, err
	}
	return n, tx.Commit(ctx)
}
