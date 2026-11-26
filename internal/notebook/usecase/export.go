package usecase

import (
	"context"
	"errors"
	"io"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

// ExportEncoder пишет согласованный IPYNB в destination, читая ячейки по одной.
type ExportEncoder func(context.Context, io.Writer, domain.Document, func(func(domain.Cell) error) error) error

// WithExport подключает форматирование IPYNB независимо от PostgreSQL.
func WithExport(encoder ExportEncoder) Option {
	return func(uc *NotebookUsecase) { uc.encodeExport = encoder }
}

// Export пишет полный сохранённый документ и возвращает его имя и ревизию.
func (uc *NotebookUsecase) Export(ctx context.Context, id, visitor int64, destination io.Writer) (string, string, error) {
	store, ok := uc.repo.(interface {
		ExportSnapshot(context.Context, int64, int64, func(domain.Document, func(func(domain.Cell) error) error) error) (domain.Notebook, error)
	})
	if !ok || uc.encodeExport == nil {
		return "", "", errors.New("notebook export unavailable")
	}
	n, err := store.ExportSnapshot(ctx, id, visitor, func(doc domain.Document, each func(func(domain.Cell) error) error) error {
		return uc.encodeExport(ctx, destination, doc, each)
	})
	return n.Name, n.Revision, err
}
